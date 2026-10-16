package web

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// sessionCookie is the name of the cookie that identifies a browser.
const sessionCookie = "et_session"

// sessionTTL is how long a browser stays signed in without making a request.
const sessionTTL = 24 * time.Hour

const maxSessions = 1000

// randomToken returns a string that cannot be guessed: 256 random bits.
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read never returns an error; a token must not be
		// guessable, so do not go on without one.
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// sessions are the browsers the server knows. A session is made when a browser
// opens the page from this computer, or after a correct password from another.
type sessions struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[string]*session
}

type session struct {
	seen time.Time
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{now: now, m: map[string]*session{}}
}

func (s *sessions) create() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) >= maxSessions {
		s.dropOldestLocked()
	}
	tok := randomToken()
	s.m[tok] = &session{seen: s.now()}
	return tok
}

func (s *sessions) dropOldestLocked() {
	var oldest string
	var at time.Time
	for tok, c := range s.m {
		if oldest == "" || c.seen.Before(at) {
			oldest, at = tok, c.seen
		}
	}
	delete(s.m, oldest)
}

// valid reports whether tok is a live session, and renews it.
func (s *sessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[tok]
	if !ok {
		return false
	}
	if s.now().Sub(c.seen) > sessionTTL {
		delete(s.m, tok)
		return false
	}
	c.seen = s.now()
	return true
}

func (s *sessions) remove(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tok)
}

func (s *sessions) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, c := range s.m {
		if s.now().Sub(c.seen) > sessionTTL {
			delete(s.m, tok)
		}
	}
}

// loginLimiter slows down password guessing: after maxFailures wrong passwords
// from one address, that address is refused for lockout.
type loginLimiter struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[string]*attempts
}

type attempts struct {
	count int
	first time.Time
	until time.Time
}

const (
	maxFailures   = 5
	failureWindow = 10 * time.Minute
	lockout       = 5 * time.Minute
)

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{now: now, m: map[string]*attempts{}}
}

// blocked reports how long ip must wait, or 0.
func (l *loginLimiter) blocked(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return 0
	}
	if d := a.until.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

func (l *loginLimiter) failed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.m) > 10_000 {
		l.m = map[string]*attempts{} // an address flood must not grow the table without bound
	}
	a, ok := l.m[ip]
	if !ok || now.Sub(a.first) > failureWindow {
		a = &attempts{first: now}
		l.m[ip] = a
	}
	a.count++
	if a.count >= maxFailures {
		a.until = now.Add(lockout)
		a.count = 0
		a.first = now
	}
}

func (l *loginLimiter) succeeded(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

// remoteIP returns the address the request came from.
func remoteIP(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

// isLocalHost reports whether a Host header names this computer.
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// verdict is the outcome of the access check of one request.
type verdict int

const (
	allowed      verdict = iota
	denied               // refused: 403
	needLogin            // another computer that has not signed in
	notSameSite          // a request that another website caused
	tooManyTries         // refused for guessing passwords
)

// accessFor decides whether a request may be served (section 4.7):
//
//   - from this computer it is served, if the page was addressed as localhost
//     or by a loopback address. A page on another website can make the browser
//     send requests to 127.0.0.1 under its own name (DNS rebinding), and the
//     Host header is what shows it;
//   - from another computer it is served only when a password is set and the
//     browser has signed in with it;
//   - a request that changes something must come from this page itself.
func (r *run) accessFor(req *http.Request) (v verdict, reason string, local bool) {
	ip := remoteIP(req)
	local = ip.IsValid() && ip.IsLoopback()
	switch {
	case !ip.IsValid():
		return denied, "unreadable address", false
	case local:
		if !isLocalHost(req.Host) {
			return denied, "host is not localhost", true
		}
	case r.opts.PasswordHash == "":
		return denied, "remote access is off", false
	case !r.sessions.valid(cookieValue(req)) && !openToUnsigned(req):
		return needLogin, "not signed in", false
	}
	if !sameSite(req) {
		return notSameSite, "cross-site request", local
	}
	return allowed, "", local
}

// openToUnsigned is what a browser that has not signed in may fetch: the login
// page and the files it uses. Nothing there depends on the user's data.
func openToUnsigned(req *http.Request) bool {
	p := req.URL.Path
	return p == "/login" || strings.HasPrefix(p, "/static/")
}

func cookieValue(req *http.Request) string {
	c, err := req.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// sameSite checks that a request that changes something was made by a page of
// this server. Browsers send Origin on such requests and Sec-Fetch-Site on
// every request; the cookie is SameSite=Strict as a third barrier.
func sameSite(req *http.Request) bool {
	if site := req.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		return false
	}
	return originHost(origin) == strings.ToLower(req.Host)
}

func originHost(origin string) string {
	origin = strings.ToLower(origin)
	if _, rest, ok := strings.Cut(origin, "://"); ok {
		return rest
	}
	return origin
}

func setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(sessionTTL / time.Second),
	})
}
