// Package web serves the English Tutor window in a browser (section 4.7 of the
// spec). The page is built with Datastar: the server keeps the state of every
// open tab, renders it as HTML, and pushes the parts that changed over a
// Server-Sent Events stream. The package has no Qt imports.
package web

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// Deps is what the server needs from the application.
type Deps struct {
	Log   *slog.Logger
	Store *store.Store
	Stats *store.Stats
	// NewModel builds the langchaingo model of a configured LLM.
	NewModel func(cfg store.LLMConfig) (llms.Model, error)
	// SettingsChanged is called, from any goroutine, after the web page changed
	// the LLM list or the general settings, so the desktop window can reload them.
	SettingsChanged func()
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Options are the settings of one run of the server.
type Options struct {
	// Port to listen on.
	Port int
	// PasswordHash is the stored hash of the password (see HashPassword). With
	// none, only this computer is served; with one, other computers are served
	// too, after they sign in.
	PasswordHash string
}

// Server is the web server. Start and Stop may be called repeatedly; the zero
// value is not usable, use New.
type Server struct {
	root context.Context
	deps Deps
	now  func() time.Time

	mu      sync.Mutex // guards the fields below
	running bool
	run     *run
}

// run is the state of one Start..Stop period.
type run struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   Options
	http   *http.Server
	lns    []net.Listener

	sessions *sessions
	limiter  *loginLimiter

	wsMu       sync.Mutex
	workspaces map[string]*workspace
	closed     bool

	wg sync.WaitGroup
}

// New returns a stopped server. Everything it starts ends when root is
// cancelled or Stop is called.
func New(root context.Context, deps Deps) *Server {
	s := &Server{root: root, deps: deps, now: deps.Now}
	if s.now == nil {
		s.now = time.Now
	}
	if s.deps.Log == nil {
		s.deps.Log = slog.New(slog.DiscardHandler)
	}
	return s
}

// Running reports whether the server is listening.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Port returns the port the server listens on, or 0 when it is stopped.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return 0
	}
	return s.run.opts.Port
}

// URL returns the address to open in a browser on this computer, or "" when the
// server is stopped.
func (s *Server) URL() string {
	if p := s.Port(); p != 0 {
		return "http://localhost:" + strconv.Itoa(p) + "/"
	}
	return ""
}

// Start listens on the port and serves in the background. The port is bound
// before Start returns, so a port that is in use is reported at once. ctx
// bounds the binding only; the server runs until Stop or the end of the root
// context given to New.
//
// Errors: WebPortInvalid, WebServerAlreadyRunning, WebServerStartFailed. Their
// messages are written for the user.
func (s *Server) Start(ctx context.Context, opts Options) error {
	if err := store.ValidateWebPort(opts.Port); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("WebServerAlreadyRunning", "The web server is already running.", "port", s.run.opts.Port)
	}
	lns, err := listen(ctx, opts.Port, opts.PasswordHash != "")
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(s.root)
	r := &run{
		ctx: runCtx, cancel: cancel, opts: opts, lns: lns,
		sessions:   newSessions(s.now),
		limiter:    newLoginLimiter(s.now),
		workspaces: map[string]*workspace{},
	}
	r.http = &http.Server{
		Handler:           s.handler(r),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return runCtx },
		ErrorLog:          slog.NewLogLogger(s.deps.Log.Handler(), slog.LevelWarn),
	}
	for _, ln := range lns {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			if err := r.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.deps.Log.Error("web server stopped", "error", err.Error())
			}
		}()
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		s.janitor(r)
	}()
	s.run = r
	s.running = true
	s.deps.Log.Info("web server started", "port", opts.Port, "remote_access", opts.PasswordHash != "")
	return nil
}

// Stop ends the stream of every open page, cancels the work they started, and
// closes the listeners. It returns when everything has ended.
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	r := s.run
	s.running = false
	s.run = nil
	s.mu.Unlock()

	r.cancel() // ends the event streams and the checks that are running
	r.closeWorkspaces()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.http.Shutdown(ctx); err != nil {
		_ = r.http.Close() // a connection that does not end is cut
	}
	r.wg.Wait()
	s.deps.Log.Info("web server stopped")
}

// listen binds the port. Without a password only the loopback addresses are
// bound, so other computers cannot even connect; with one, every address is.
func listen(ctx context.Context, port int, remote bool) ([]net.Listener, error) {
	addr := func(host string) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
	var lc net.ListenConfig
	if remote {
		ln, err := lc.Listen(ctx, "tcp", addr(""))
		if err != nil {
			return nil, listenFailed(port, err)
		}
		return []net.Listener{ln}, nil
	}
	v4, err := lc.Listen(ctx, "tcp4", addr("127.0.0.1"))
	if err != nil {
		return nil, listenFailed(port, err)
	}
	lns := []net.Listener{v4}
	// "localhost" may resolve to ::1 first; a computer without IPv6 simply
	// has no second listener.
	if v6, err := lc.Listen(ctx, "tcp6", addr("::1")); err == nil {
		lns = append(lns, v6)
	}
	return lns, nil
}

func listenFailed(port int, err error) error {
	reason := err.Error()
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		reason = op.Err.Error()
	}
	return errors.NewErr("WebServerStartFailed", err).SetMetadata("port", port).
		SetMessage("The web server could not start on port " + strconv.Itoa(port) + ": " + reason + ".")
}

// workspaceLimit bounds the number of open pages.
const workspaceLimit = 64

// idleAfter is how long a page that no browser is connected to is kept.
const idleAfter = 12 * time.Hour

// janitor removes the workspaces nobody has used for a long time.
func (s *Server) janitor(r *run) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			r.sweep(s.now(), idleAfter)
			r.sessions.sweep()
		}
	}
}

// spawn runs f in a goroutine that Stop waits for. It reports false, and runs
// nothing, once the server is stopping.
func (r *run) spawn(f func()) bool {
	r.wsMu.Lock()
	defer r.wsMu.Unlock()
	if r.closed {
		return false
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		f()
	}()
	return true
}

func (r *run) closeWorkspaces() {
	r.wsMu.Lock()
	r.closed = true
	all := r.workspaces
	r.workspaces = map[string]*workspace{}
	r.wsMu.Unlock()
	for _, w := range all {
		w.close()
	}
}

func (r *run) sweep(now time.Time, idle time.Duration) {
	var gone []*workspace
	r.wsMu.Lock()
	for id, w := range r.workspaces {
		if w.idleSince(now) > idle {
			delete(r.workspaces, id)
			gone = append(gone, w)
		}
	}
	r.wsMu.Unlock()
	for _, w := range gone {
		w.close()
	}
}

// workspaceFor returns the page state with this id, if it belongs to the session.
func (r *run) workspaceFor(id, session string) (*workspace, bool) {
	r.wsMu.Lock()
	defer r.wsMu.Unlock()
	w, ok := r.workspaces[id]
	if !ok || w.owner != session {
		return nil, false
	}
	return w, true
}

// addWorkspace registers w, making room by dropping the longest idle page that
// no browser is connected to. It reports false when there is no room.
func (r *run) addWorkspace(w *workspace, now time.Time) bool {
	var evicted *workspace
	r.wsMu.Lock()
	if r.closed {
		r.wsMu.Unlock()
		return false
	}
	if len(r.workspaces) >= workspaceLimit {
		var oldestID string
		var oldest time.Duration = -1
		for id, c := range r.workspaces {
			if d := c.idleSince(now); !c.connected() && d > oldest {
				oldestID, oldest = id, d
			}
		}
		if oldest < 0 {
			r.wsMu.Unlock()
			return false
		}
		evicted = r.workspaces[oldestID]
		delete(r.workspaces, oldestID)
	}
	r.workspaces[w.id] = w
	r.wsMu.Unlock()
	if evicted != nil {
		evicted.close()
	}
	return true
}
