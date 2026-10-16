package web

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// quickHash is the hash of a password with few rounds, so tests are fast.
func quickHash(t *testing.T, password string) string {
	t.Helper()
	salt := []byte("0123456789abcdef")
	sum, err := pbkdf2.Key(sha256.New, password, salt, 1000, 32)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join([]string{hashPrefix, "1000",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum)}, "$")
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// testRun is a run that does not listen: requests are handed to its handler
// directly, so they can come from any address.
type testRun struct {
	*run
	srv   *Server
	h     http.Handler
	clock *fakeClock
	logs  *syncBuffer
}

func newTestRun(t *testing.T, opts Options) *testRun {
	t.Helper()
	st, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := &fakeClock{t: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	logs := &syncBuffer{}
	srv := New(context.Background(), Deps{
		Log: slog.New(slog.NewTextHandler(logs, nil)), Store: st, Stats: store.NewStats(st, nil),
		NewModel: func(store.LLMConfig) (llms.Model, error) { return llmtest.New(), nil },
		Now:      clock.now,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &run{ctx: ctx, cancel: cancel, opts: opts, sessions: newSessions(clock.now), limiter: newLoginLimiter(clock.now),
		workspaces: map[string]*workspace{}}
	return &testRun{run: r, srv: srv, h: srv.handler(r), clock: clock, logs: logs}
}

type req struct {
	method, path, remote, host string
	header                     map[string]string
	cookie                     string
	body                       string
}

func (tr *testRun) do(r req) *httptest.ResponseRecorder {
	if r.method == "" {
		r.method = "GET"
	}
	if r.remote == "" {
		r.remote = "127.0.0.1:50000"
	}
	if r.host == "" {
		r.host = "localhost:3333"
	}
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	hr.RemoteAddr, hr.Host = r.remote, r.host
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: sessionCookie, Value: r.cookie})
	}
	rec := httptest.NewRecorder()
	tr.h.ServeHTTP(rec, hr)
	return rec
}

func TestAccessDecisions(t *testing.T) {
	const remote = "192.0.2.10:40000"
	withPassword := Options{Port: 3333, PasswordHash: "x"}
	noPassword := Options{Port: 3333}
	sameOrigin := map[string]string{"Origin": "http://localhost:3333", "Sec-Fetch-Site": "same-origin"}

	tests := []struct {
		name string
		opts Options
		req  req
		want verdict
	}{
		{"localhost", noPassword, req{path: "/"}, allowed},
		{"127.0.0.1 by number", noPassword, req{path: "/", host: "127.0.0.1:3333"}, allowed},
		{"::1 by number", noPassword, req{path: "/", host: "[::1]:3333", remote: "[::1]:50000"}, allowed},
		{"an ipv4 address inside ipv6", noPassword, req{path: "/", remote: "[::ffff:127.0.0.1]:50000"}, allowed},
		{"a name under .localhost", noPassword, req{path: "/", host: "tutor.localhost:3333"}, allowed},
		{"a site that points its name at 127.0.0.1 (DNS rebinding)", noPassword, req{path: "/", host: "evil.example:3333"}, denied},
		{"a name that only starts with localhost", noPassword, req{path: "/", host: "localhost.evil.example"}, denied},
		{"no host header", noPassword, req{path: "/", host: "-"}, denied},
		{"another computer, no password", noPassword, req{path: "/", remote: remote, host: "192.0.2.1:3333"}, denied},
		{"another computer, no password, even the login page", noPassword, req{path: "/login", remote: remote, host: "192.0.2.1:3333"}, denied},
		{"another computer, password, not signed in", withPassword, req{path: "/", remote: remote, host: "192.0.2.1:3333"}, needLogin},
		{"another computer, the login page", withPassword, req{path: "/login", remote: remote, host: "192.0.2.1:3333"}, allowed},
		{"another computer, the files of the login page", withPassword, req{path: "/static/app.css", remote: remote, host: "192.0.2.1:3333"}, allowed},
		{"another computer, a stream, not signed in", withPassword, req{path: "/w/abc/events", remote: remote, host: "192.0.2.1:3333"}, needLogin},
		{"this computer needs no password", withPassword, req{path: "/"}, allowed},
		{"unreadable address", noPassword, req{path: "/", remote: "nonsense"}, denied},

		{"a change with the page's own origin", noPassword, req{method: "POST", path: "/x", header: sameOrigin}, allowed},
		{"a change without an origin", noPassword, req{method: "POST", path: "/x"}, notSameSite},
		{"a change from another site", noPassword, req{method: "POST", path: "/x", header: map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"}}, notSameSite},
		{"a change from another site that hides the fetch metadata", noPassword, req{method: "POST", path: "/x", header: map[string]string{"Origin": "http://evil.example"}}, notSameSite},
		{"a change from another port", noPassword, req{method: "POST", path: "/x", header: map[string]string{"Origin": "http://localhost:4444"}}, notSameSite},
		{"a change from a sibling site", noPassword, req{method: "POST", path: "/x", header: map[string]string{"Origin": "http://localhost:3333", "Sec-Fetch-Site": "same-site"}}, notSameSite},
		{"a cross-site page load", noPassword, req{path: "/", header: map[string]string{"Sec-Fetch-Site": "cross-site"}}, notSameSite},
		{"typing the address", noPassword, req{path: "/", header: map[string]string{"Sec-Fetch-Site": "none"}}, allowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTestRun(t, tt.opts)
			req := tt.req
			hr := httptest.NewRequest(orDefault(req.method, "GET"), req.path, nil)
			hr.RemoteAddr, hr.Host = orDefault(req.remote, "127.0.0.1:50000"), orDefault(req.host, "localhost:3333")
			if req.host == "-" {
				hr.Host = ""
			}
			for k, v := range req.header {
				hr.Header.Set(k, v)
			}
			if got, reason, _ := tr.accessFor(hr); got != tt.want {
				t.Errorf("accessFor = %v (%s), want %v", got, reason, tt.want)
			}
		})
	}
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func TestSignedInBrowserOfAnotherComputerIsServed(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333, PasswordHash: quickHash(t, "secret-pass")})
	const remote, host = "192.0.2.10:40000", "192.0.2.1:3333"
	tok := tr.sessions.create()

	rec := tr.do(req{path: "/login", remote: remote, host: host})
	if rec.Code != 200 {
		t.Fatalf("login page = %d", rec.Code)
	}
	if rec := tr.do(req{path: "/", remote: remote, host: host, cookie: "made-up"}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("a made-up cookie = %d %q; want a redirect to /login", rec.Code, rec.Header().Get("Location"))
	}
	if rec := tr.do(req{path: "/", remote: remote, host: host, cookie: tok}); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/w/") {
		t.Errorf("a signed-in browser = %d %q; want a redirect to its page", rec.Code, rec.Header().Get("Location"))
	}
	// A session ends after a day without requests.
	tr.clock.t = tr.clock.t.Add(sessionTTL + time.Minute)
	if rec := tr.do(req{path: "/", remote: remote, host: host, cookie: tok}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("an old session = %d %q; want the login page", rec.Code, rec.Header().Get("Location"))
	}
	// A request from a stream (Datastar) is refused instead of redirected.
	rec = tr.do(req{path: "/w/x/events", remote: remote, host: host, header: map[string]string{"Datastar-Request": "true"}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a stream without a session = %d, want 401", rec.Code)
	}
}

func TestLoginFlow(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333, PasswordHash: quickHash(t, "secret-pass")})
	const remote, host = "192.0.2.10:40000", "192.0.2.1:3333"
	post := func(pw string) *httptest.ResponseRecorder {
		return tr.do(req{
			method: "POST", path: "/login", remote: remote, host: host,
			header: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://" + host},
			body:   url.Values{"password": {pw}}.Encode(),
		})
	}

	if rec := post("wrong"); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Wrong password.") {
		t.Errorf("a wrong password = %d %q", rec.Code, rec.Body.String())
	}
	rec := post("secret-pass")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("the right password = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || len(cookie.Value) < 40 {
		t.Fatalf("the session cookie: %+v", cookie)
	}
	if rec := tr.do(req{path: "/", remote: remote, host: host, cookie: cookie.Value}); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/w/") {
		t.Errorf("after signing in: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Signing out ends the session.
	out := tr.do(req{method: "POST", path: "/logout", remote: remote, host: host, cookie: cookie.Value,
		header: map[string]string{"Origin": "http://" + host}})
	if out.Code != http.StatusSeeOther || out.Header().Get("Location") != "/login" {
		t.Errorf("logout = %d %q", out.Code, out.Header().Get("Location"))
	}
	if rec := tr.do(req{path: "/", remote: remote, host: host, cookie: cookie.Value}); rec.Header().Get("Location") != "/login" {
		t.Error("the session must be gone after logout")
	}
}

func TestLoginRefusesAfterTooManyWrongPasswords(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333, PasswordHash: quickHash(t, "secret-pass")})
	const host = "192.0.2.1:3333"
	post := func(remote, pw string) *httptest.ResponseRecorder {
		return tr.do(req{
			method: "POST", path: "/login", remote: remote, host: host,
			header: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://" + host},
			body:   url.Values{"password": {pw}}.Encode(),
		})
	}
	for i := range maxFailures {
		if rec := post("192.0.2.10:1", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("try %d = %d", i+1, rec.Code)
		}
	}
	// Now even the right password is refused.
	rec := post("192.0.2.10:2", "secret-pass")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("after %d failures = %d %v; want 429 with Retry-After", maxFailures, rec.Code, rec.Header())
	}
	// Another address is not affected.
	if rec := post("192.0.2.99:1", "secret-pass"); rec.Code != http.StatusSeeOther {
		t.Errorf("another address = %d", rec.Code)
	}
	// The block ends.
	tr.clock.t = tr.clock.t.Add(lockout + time.Second)
	if rec := post("192.0.2.10:3", "secret-pass"); rec.Code != http.StatusSeeOther {
		t.Errorf("after the lockout = %d", rec.Code)
	}
}

func TestLoginPageIsNotThereWithoutAPassword(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333})
	if rec := tr.do(req{path: "/login"}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("GET /login without a password = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := tr.do(req{method: "POST", path: "/login", header: map[string]string{"Origin": "http://localhost:3333", "Content-Type": "application/x-www-form-urlencoded"}, body: "password=x"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /login without a password = %d, want 403", rec.Code)
	}
}

func TestEveryRequestIsLogged(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333, PasswordHash: quickHash(t, "secret-pass")})
	tr.do(req{path: "/"})                                             // allowed, from this computer
	tr.do(req{path: "/", host: "evil.example"})                       // denied: rebinding
	tr.do(req{path: "/", remote: "192.0.2.10:40000", host: "h:3333"}) // needs the login
	tr.do(req{path: "/nothing-here"})                                 // not found
	tr.do(req{method: "POST", path: "/w/SECRETWORKSPACEID/text?pos=7", header: map[string]string{"Origin": "http://evil.example"},
		body: `{"text":"my private essay"}`}) // denied, with a body and a query

	log := tr.logs.String()
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 5 {
		t.Fatalf("want one log line per request (5), got %d:\n%s", len(lines), log)
	}
	for _, want := range []string{
		`level=INFO msg="web request" method=GET route="GET /{$}" remote=127.0.0.1 status=303`,
		`level=WARN msg="web request" method=GET route="GET /" remote=127.0.0.1 status=403`,
		`reason="host is not localhost"`,
		`remote=192.0.2.10 status=303`, `reason="not signed in"`,
		`route="GET /nothing-here" remote=127.0.0.1 status=404`,
		`route="POST /w/{id}/text"`, `reason="cross-site request"`,
		"access=allowed", "access=denied",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	for _, line := range lines {
		for _, field := range []string{"method=", "route=", "remote=", "status=", "bytes=", "ms=", "access="} {
			if !strings.Contains(line, field) {
				t.Errorf("the line lacks %s: %s", field, line)
			}
		}
	}
	for _, secret := range []string{"SECRETWORKSPACEID", "pos=7", "my private essay", "secret-pass"} {
		if strings.Contains(log, secret) {
			t.Errorf("the log must not contain %q:\n%s", secret, log)
		}
	}
}

func TestLogPathHidesTheKeysToAPage(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/", "/"},
		{"/w/abc123/", "/w/{id}/"},
		{"/w/abc123/events", "/w/{id}/events"},
		{"/w/abc123/download/tok456", "/w/{id}/download/{token}"},
		{"/static/app.js", "/static/app.js"},
		{"/" + strings.Repeat("a", 500), "/" + strings.Repeat("a", 199) + "..."},
	}
	for _, tt := range tests {
		if got := logPath(tt.in); got != tt.want {
			t.Errorf("logPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResponsesCarrySecurityHeaders(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333})
	for _, path := range []string{"/", "/static/app.js", "/nothing-here", "/login"} {
		h := tr.do(req{path: path}).Header()
		for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
			if h.Get(name) == "" {
				t.Errorf("%s lacks %s", path, name)
			}
		}
		if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			strings.Contains(h.Get("Content-Security-Policy"), "unsafe-inline") {
			t.Errorf("%s: unexpected policy %q", path, h.Get("Content-Security-Policy"))
		}
	}
}

func TestStaticFiles(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333})
	tests := []struct{ path, typePrefix, contains string }{
		{"/static/datastar.js", "text/javascript", "Datastar v1.0.4"},
		{"/static/app.js", "text/javascript", "window.et"},
		{"/static/app.css", "text/css", "--grammar"},
	}
	for _, tt := range tests {
		rec := tr.do(req{path: tt.path})
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), tt.typePrefix) || !strings.Contains(rec.Body.String(), tt.contains) {
			t.Errorf("%s = %d %q", tt.path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := tr.do(req{path: "/static/nothing.js"}); rec.Code != 404 {
		t.Errorf("a missing file = %d", rec.Code)
	}
	if rec := tr.do(req{path: "/static/../go.mod"}); rec.Code == 200 {
		t.Error("static files must stay inside the folder")
	}
}

// --- over a real connection ---

// lanAddress finds an address of this computer that is not a loopback address,
// to connect from "another computer".
func lanAddress(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skip("no network interfaces:", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			return ipn.IP.String()
		}
	}
	t.Skip("this computer has no address besides the loopback one")
	return ""
}

func TestAnotherComputerNeedsThePassword(t *testing.T) {
	ip := lanAddress(t)
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, llmtest.New(), Options{PasswordHash: hash})
	host := net.JoinHostPort(ip, strconv.Itoa(e.port))
	b := e.browser()
	do := func(method, path, body string, header map[string]string) (int, string, http.Header) {
		hr, err := http.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range header {
			hr.Header.Set(k, v)
		}
		resp, err := b.http.Do(hr)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data), resp.Header
	}

	if code, _, h := do("GET", "/", "", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("GET / from another computer = %d %q; want the login page", code, h.Get("Location"))
	}
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://" + host}
	if code, body, _ := do("POST", "/login", "password=wrong", form); code != 401 || !strings.Contains(body, "Wrong password.") {
		t.Fatalf("wrong password = %d", code)
	}
	if code, _, h := do("POST", "/login", "password=correct+horse", form); code != 303 || h.Get("Location") != "/" {
		t.Fatalf("right password = %d %q", code, h.Get("Location"))
	}
	code, _, h := do("GET", "/", "", nil)
	if code != 303 || !strings.HasPrefix(h.Get("Location"), "/w/") {
		t.Fatalf("after signing in = %d %q", code, h.Get("Location"))
	}
	if code, body, _ := do("GET", h.Get("Location"), "", nil); code != 200 || !strings.Contains(body, "Sign out") {
		t.Errorf("the page of a remote browser has a Sign out button: %d", code)
	}
	// This computer is not asked for the password.
	local := e.browser()
	local.open()
}

func TestWithoutAPasswordOnlyTheLoopbackAddressListens(t *testing.T) {
	ip := lanAddress(t)
	e := newEnv(t, llmtest.New(), Options{})
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(e.port)), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("the server accepted a connection on %s without a password", ip)
	}
	b := e.browser()
	b.open() // localhost works
}
