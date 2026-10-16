package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// env is a running server with an in-memory database and a fake LLM.
type env struct {
	t     *testing.T
	st    *store.Store
	stats *store.Stats
	fake  *llmtest.Fake
	srv   *Server
	logs  *syncBuffer
	port  int
	llmID uint

	changed chan struct{} // SettingsChanged was called
}

// freePort finds a port nobody listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func newEnv(t *testing.T, fake *llmtest.Fake, opts Options) *env {
	t.Helper()
	st, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := &store.LLMConfig{Name: "local", Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
	if err := st.CreateLLMConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, stats: store.NewStats(st, nil), fake: fake, logs: &syncBuffer{}, llmID: cfg.ID, changed: make(chan struct{}, 100)}
	root, cancel := context.WithCancel(context.Background())
	e.srv = New(root, Deps{
		Log:   slog.New(slog.NewTextHandler(e.logs, nil)),
		Store: st, Stats: e.stats,
		NewModel:        func(store.LLMConfig) (llms.Model, error) { return fake, nil },
		SettingsChanged: func() { e.changed <- struct{}{} },
	})
	if opts.Port == 0 {
		opts.Port = freePort(t)
	}
	e.port = opts.Port
	if err := e.srv.Start(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		e.srv.Stop()
		cancel()
	})
	return e
}

func (e *env) base() string { return fmt.Sprintf("http://localhost:%d", e.port) }

// workspaces returns the open pages.
func (e *env) workspaces() []*workspace {
	e.srv.mu.Lock()
	r := e.srv.run
	e.srv.mu.Unlock()
	r.wsMu.Lock()
	defer r.wsMu.Unlock()
	var out []*workspace
	for _, w := range r.workspaces {
		out = append(out, w)
	}
	return out
}

// browser is a client with its own cookies.
type browser struct {
	t    *testing.T
	e    *env
	http *http.Client
}

func (e *env) browser() *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return &browser{t: e.t, e: e, http: &http.Client{
		Jar:           jar,
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) do(method, path string, body io.Reader, header map[string]string) *http.Response {
	b.t.Helper()
	req, err := http.NewRequest(method, b.e.base()+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (b *browser) get(path string) (int, string) {
	b.t.Helper()
	resp := b.do("GET", path, nil, nil)
	return resp.StatusCode, readBody(b.t, resp)
}

// open does what a browser does when the address is typed: it follows the
// redirect from / to the page of its own.
func (b *browser) open() (id, html string) {
	b.t.Helper()
	resp := b.do("GET", "/", nil, nil)
	loc := resp.Header.Get("Location")
	_ = readBody(b.t, resp)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/w/") {
		b.t.Fatalf("GET / = %d, Location %q; want a redirect to a page", resp.StatusCode, loc)
	}
	status, html := b.get(loc)
	if status != http.StatusOK {
		b.t.Fatalf("GET %s = %d", loc, status)
	}
	return strings.Split(loc, "/")[2], html
}

// post sends an action the way Datastar does: with the signals as JSON.
func (b *browser) post(id, action string, text string) int {
	b.t.Helper()
	body, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		b.t.Fatal(err)
	}
	resp := b.do("POST", "/w/"+id+"/"+action, bytes.NewReader(body), b.actionHeaders("application/json"))
	_ = readBody(b.t, resp)
	return resp.StatusCode
}

// postForm sends a form, as Datastar does for a dialog.
func (b *browser) postForm(id, action string, form url.Values) int {
	b.t.Helper()
	resp := b.do("POST", "/w/"+id+"/"+action, strings.NewReader(form.Encode()), b.actionHeaders("application/x-www-form-urlencoded"))
	_ = readBody(b.t, resp)
	return resp.StatusCode
}

// upload sends a file the way the Open button does.
func (b *browser) upload(id, name string, data []byte) int {
	b.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		b.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		b.t.Fatal(err)
	}
	resp := b.do("POST", "/w/"+id+"/open", &buf, b.actionHeaders(mw.FormDataContentType()))
	_ = readBody(b.t, resp)
	return resp.StatusCode
}

func (b *browser) actionHeaders(contentType string) map[string]string {
	return map[string]string{
		"Content-Type":     contentType,
		"Datastar-Request": "true",
		"Origin":           b.e.base(),
		"Sec-Fetch-Site":   "same-origin",
	}
}

// sseEvent is one event of the stream.
type sseEvent struct {
	Name string
	Data string
}

// regionMarkers start the parts of the page, in the order they are sent.
var regionMarkers = []struct{ name, marker string }{
	{"toolbar", `<header id="toolbar"`},
	{"banner", `<div id="banner"`},
	{"underlay", `<div id="underlay"`},
	{"issues", `<section id="issues"`},
	{"explain", `<section id="explain"`},
	{"status", `<footer id="statusbar"`},
	{"modal", `<div id="modal"`},
	{"download", `<div id="download"`},
}

// eventStream is an open event stream of a page. It keeps what the page would
// show: the latest HTML of each part and the latest value of each signal.
type eventStream struct {
	events  chan sseEvent
	cancel  context.CancelFunc
	done    chan struct{}
	regions map[string]string
	signals map[string]any
	all     []sseEvent // every event consumed, for assertions about what was sent
}

func (b *browser) stream(id string) *eventStream {
	b.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", b.e.base()+"/w/"+id+"/events", nil)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	client := &http.Client{Jar: b.http.Jar}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		b.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		b.t.Fatalf("events = %d", resp.StatusCode)
	}
	s := &eventStream{events: make(chan sseEvent, 1000), cancel: cancel, done: make(chan struct{}),
		regions: map[string]string{}, signals: map[string]any{}}
	go func() {
		defer close(s.done)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		var ev sseEvent
		var data []string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.Name != "" {
					ev.Data = strings.Join(data, "\n")
					s.events <- ev
				}
				ev, data = sseEvent{}, nil
			case strings.HasPrefix(line, "event: "):
				ev.Name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = append(data, strings.TrimPrefix(line, "data: "))
			}
		}
	}()
	b.t.Cleanup(cancel)
	return s
}

// apply updates what the page shows with one event.
func (s *eventStream) apply(t *testing.T, ev sseEvent) {
	t.Helper()
	s.all = append(s.all, ev)
	switch ev.Name {
	case "datastar-patch-elements":
		var lines []string
		for _, l := range strings.Split(ev.Data, "\n") {
			if rest, ok := strings.CutPrefix(l, "elements "); ok {
				lines = append(lines, rest)
			}
		}
		html := strings.Join(lines, "\n")
		type found struct {
			name string
			at   int
		}
		var marks []found
		for _, m := range regionMarkers {
			if at := strings.Index(html, m.marker); at >= 0 {
				marks = append(marks, found{m.name, at})
			}
		}
		for i, m := range marks {
			end := len(html)
			for _, o := range marks {
				if o.at > m.at && o.at < end {
					end = o.at
				}
			}
			_ = i
			s.regions[m.name] = html[m.at:end]
		}
	case "datastar-patch-signals":
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(ev.Data, "signals ")), &m); err != nil {
			t.Fatalf("bad signals patch %q: %v", ev.Data, err)
		}
		for k, v := range m {
			s.signals[k] = v
		}
	}
}

// waitRegion waits until a part of the page contains every fragment.
func (s *eventStream) waitRegion(t *testing.T, region string, fragments ...string) string {
	t.Helper()
	has := func() bool {
		for _, f := range fragments {
			if !strings.Contains(s.regions[region], f) {
				return false
			}
		}
		return len(s.regions[region]) > 0
	}
	deadline := time.After(5 * time.Second)
	for !has() {
		select {
		case ev := <-s.events:
			s.apply(t, ev)
		case <-deadline:
			t.Fatalf("timed out waiting for %s to contain %q; it is %q", region, fragments, s.regions[region])
		case <-s.done:
			t.Fatalf("the stream ended while waiting for %s to contain %q", region, fragments)
		}
	}
	return s.regions[region]
}

// waitSignal waits until a signal has a value, compared as JSON.
func (s *eventStream) waitSignal(t *testing.T, key string, want any) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	has := func() bool {
		got, ok := s.signals[key]
		if !ok {
			return false
		}
		gotJSON, err := json.Marshal(got)
		return err == nil && string(gotJSON) == string(wantJSON)
	}
	deadline := time.After(5 * time.Second)
	for !has() {
		select {
		case ev := <-s.events:
			s.apply(t, ev)
		case <-deadline:
			t.Fatalf("timed out waiting for signal %s = %s; it is %v", key, wantJSON, s.signals[key])
		case <-s.done:
			t.Fatalf("the stream ended while waiting for signal %s", key)
		}
	}
}

// quiet applies events until none arrives for a while.
func (s *eventStream) quiet(t *testing.T) {
	t.Helper()
	for {
		select {
		case ev := <-s.events:
			s.apply(t, ev)
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}

// eventually polls cond until it holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// with runs f with the workspace locked.
func (w *workspace) with(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f()
}

func issuesReply(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, `{"text":"`+pairs[i]+`","error_type":"`+pairs[i+1]+`","message":"msg for `+pairs[i]+`"}`)
	}
	return `{"issues":[` + strings.Join(parts, ",") + `]}`
}

var tokenInfo = map[string]any{"PromptTokens": 120, "CompletionTokens": 30}

const sampleText = "He go home. She have a cat."

func uitoa(n uint) string { return fmt.Sprintf("%d", n) }

func addLLM(t *testing.T, e *env, name string) uint {
	t.Helper()
	cfg := &store.LLMConfig{Name: name, Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
	if err := e.st.CreateLLMConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.ID
}
