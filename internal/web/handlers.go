package web

import (
	"embed"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var staticEmbed embed.FS

// staticFS holds datastar.js, app.js and app.css.
var staticFS = func() fs.FS {
	sub, err := fs.Sub(staticEmbed, "static")
	if err != nil {
		panic(err) // the directory is embedded above
	}
	return sub
}()

// securityHeaders are set on every response. Datastar evaluates the expressions
// in the page's attributes, which is why script-src allows 'unsafe-eval'; no
// other script runs, and the page has no inline script or style.
func securityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-eval'; style-src 'self'; "+
		"img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Cache-Control", "no-store")
}

// recorder remembers what a handler answered, for the log.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer (to flush a stream).
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// handler is the whole web application: the routes, behind the access check
// and the request log.
func (s *Server) handler(r *run) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home(r))
	mux.HandleFunc("GET /login", s.loginPage(r))
	mux.HandleFunc("POST /login", s.loginSubmit(r))
	mux.HandleFunc("POST /logout", s.logout(r))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /w/{id}/{$}", s.page(r))
	mux.HandleFunc("GET /w/{id}/events", s.events(r))
	mux.HandleFunc("GET /w/{id}/download/{token}", s.download(r))
	s.actions(r, mux)
	return s.guard(r, mux)
}

// guard checks who is asking, answers if they may not, and logs every request.
// The log has the method, the route, the address, the answer and the time. It
// has no query string, no body and no workspace address: those hold the user's
// text and the key to their page.
func (s *Server) guard(r *run, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := s.now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		securityHeaders(rec.Header())

		verdict, reason, _ := r.accessFor(req)
		switch verdict {
		case allowed:
			next.ServeHTTP(rec, req)
		case needLogin:
			if req.Method == http.MethodGet && req.Header.Get("Datastar-Request") == "" {
				http.Redirect(rec, req, "/login", http.StatusSeeOther)
			} else {
				http.Error(rec, "Sign in first.", http.StatusUnauthorized)
			}
		default:
			http.Error(rec, "Forbidden.", http.StatusForbidden)
		}

		route := req.Pattern
		if route == "" {
			route = req.Method + " " + logPath(req.URL.Path)
		}
		level, access := slog.LevelInfo, "allowed"
		if verdict != allowed {
			level, access = slog.LevelWarn, "denied"
		}
		attrs := []any{
			"method", req.Method, "route", route, "remote", remoteIP(req).String(),
			"status", rec.status, "bytes", rec.bytes, "ms", s.now().Sub(started).Milliseconds(),
			"access", access,
		}
		if reason != "" {
			attrs = append(attrs, "reason", reason)
		}
		s.deps.Log.Log(req.Context(), level, "web request", attrs...)
	})
}

// logPath is the path of a request for the log: the addresses that give access
// to a page are replaced, and the length is bounded.
func logPath(p string) string {
	parts := strings.Split(p, "/")
	for i := 1; i < len(parts)-1; i++ {
		switch parts[i] {
		case "w":
			parts[i+1] = "{id}"
		case "download":
			parts[i+1] = "{token}"
		}
	}
	p = strings.Join(parts, "/")
	if len(p) > 200 {
		p = p[:200] + "..."
	}
	return p
}

// --- pages ---

// home makes a page of its own for this browser and goes to it.
func (s *Server) home(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tok := cookieValue(req)
		if !r.sessions.valid(tok) {
			tok = r.sessions.create()
			setSessionCookie(w, tok)
		}
		ws := s.newWorkspace(r, tok)
		if !r.addWorkspace(ws, s.now()) {
			http.Error(w, "Too many pages are open. Close one and try again.", http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, req, "/w/"+ws.id+"/", http.StatusSeeOther)
	}
}

func isRemote(req *http.Request) bool { return !remoteIP(req).IsLoopback() }

func (s *Server) page(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ws, ok := r.workspaceFor(req.PathValue("id"), cookieValue(req))
		if !ok {
			http.Redirect(w, req, "/", http.StatusSeeOther) // a page from before a restart
			return
		}
		ws.mu.Lock()
		ws.lastSeen = s.now()
		body := ws.pageHTML(isRemote(req))
		ws.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.WriteString(w, body); err != nil {
			s.deps.Log.Debug("web page not sent", "error", err.Error())
		}
	}
}

// download hands the browser the file it was told about, once.
func (s *Server) download(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ws, ok := r.workspaceFor(req.PathValue("id"), cookieValue(req))
		if !ok {
			http.NotFound(w, req)
			return
		}
		d, ok := ws.takeDownload(req.PathValue("token"))
		if !ok {
			http.NotFound(w, req)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Disposition", contentDisposition(d.name))
		h.Set("Content-Length", strconv.Itoa(len(d.content)))
		if _, err := w.Write(d.content); err != nil {
			s.deps.Log.Debug("web download not sent", "error", err.Error())
		}
	}
}

// --- the event stream ---

// events keeps the page up to date: whatever changes in the workspace is
// rendered and pushed here. A browser that connects gets the whole page again,
// so a tab that was hidden catches up when it comes back.
func (s *Server) events(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Datastar-Request") != "true" {
			http.Error(w, "Bad request.", http.StatusBadRequest)
			return
		}
		ws, ok := r.workspaceFor(req.PathValue("id"), cookieValue(req))
		if !ok {
			http.Error(w, "This page is no longer available.", http.StatusNotFound)
			return
		}
		st, err := newStream(w)
		if err != nil {
			http.Error(w, "Streaming is not supported.", http.StatusInternalServerError)
			return
		}
		remote := isRemote(req)
		sub := ws.subscribe()
		defer ws.unsubscribe(sub)
		ws.mu.Lock()
		sentGoto := ws.gotoSeq // a request to move the cursor is not repeated for a browser that reconnects
		ws.mu.Unlock()

		keepAlive := time.NewTicker(20 * time.Second)
		defer keepAlive.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case <-sub.done:
				return
			case <-keepAlive.C:
				if st.comment("keep-alive") != nil {
					return
				}
				continue
			case <-sub.wake:
			}
			dirty := ws.take(sub)
			if dirty == 0 {
				continue
			}
			ws.mu.Lock()
			elements, signals := ws.render(dirty, remote, &sentGoto)
			ws.mu.Unlock()
			if elements != "" && st.patchElements(elements) != nil {
				return
			}
			if len(signals) > 0 && st.patchSignals(signals) != nil {
				return
			}
			// A streaming explanation changes much more often than anyone can
			// read it: about ten sends a second (section 4.4).
			select {
			case <-time.After(renderInterval):
			case <-req.Context().Done():
				return
			case <-sub.done:
				return
			}
		}
	}
}

// --- password ---

type loginData struct{ Error string }

func (s *Server) loginPage(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if r.opts.PasswordHash == "" || r.sessions.valid(cookieValue(req)) {
			http.Redirect(w, req, "/", http.StatusSeeOther)
			return
		}
		s.writeLogin(w, http.StatusOK, "")
	}
}

func (s *Server) writeLogin(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, execute("login", loginData{Error: msg})); err != nil {
		s.deps.Log.Debug("web login page not sent", "error", err.Error())
	}
}

// loginSubmit checks the password. Failures are counted per address; after five
// the address is refused for a while.
func (s *Server) loginSubmit(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if r.opts.PasswordHash == "" {
			http.Error(w, "Forbidden.", http.StatusForbidden)
			return
		}
		ip := remoteIP(req).String()
		if wait := r.limiter.blocked(ip); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			s.writeLogin(w, http.StatusTooManyRequests, "Too many wrong passwords. Try again in a few minutes.")
			s.deps.Log.Warn("web login refused", "remote", ip, "reason", "too many failures")
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, 8<<10)
		if err := req.ParseForm(); err != nil {
			http.Error(w, "Bad request.", http.StatusBadRequest)
			return
		}
		if !CheckPassword(r.opts.PasswordHash, req.PostForm.Get("password")) {
			r.limiter.failed(ip)
			s.deps.Log.Warn("web login failed", "remote", ip)
			s.writeLogin(w, http.StatusUnauthorized, "Wrong password.")
			return
		}
		r.limiter.succeeded(ip)
		setSessionCookie(w, r.sessions.create())
		s.deps.Log.Info("web login", "remote", ip)
		http.Redirect(w, req, "/", http.StatusSeeOther)
	}
}

func (s *Server) logout(r *run) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.sessions.remove(cookieValue(req))
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, req, "/login", http.StatusSeeOther)
	}
}
