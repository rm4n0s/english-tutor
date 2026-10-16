package app

import (
	"context"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/ui"
	"github.com/rm4n0s/english-tutor/internal/web"
)

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

// webHarness is a harness whose web server uses a free port.
func webHarness(t *testing.T, replies ...llmtest.Reply) (*harness, int) {
	t.Helper()
	h, _ := newSavingHarness(t, replies...)
	port := freePort(t)
	if err := h.st.SetWebPort(context.Background(), port); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onUI(func() { h.a.web.Stop() }) })
	return h, port
}

func httpGet(t *testing.T, port int, path string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get("http://localhost:" + strconv.Itoa(port) + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := new(strings.Builder)
	b := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, buf.String()
}

func TestWebButtonStartsAndStopsTheServer(t *testing.T) {
	h, port := webHarness(t)
	onUI(func() {
		if h.a.win.WebButtonText() != "Start web server" {
			t.Errorf("button = %q", h.a.win.WebButtonText())
		}
		h.a.win.WebButtonClick()
		if !h.a.web.Running() || h.a.win.WebButtonText() != "Stop web server" {
			t.Errorf("running=%v button=%q", h.a.web.Running(), h.a.win.WebButtonText())
		}
	})
	if code, _ := httpGet(t, port, "/"); code != http.StatusSeeOther {
		t.Errorf("GET / = %d, want a redirect to the page", code)
	}
	onUI(func() {
		h.a.win.WebButtonClick()
		if h.a.web.Running() || h.a.win.WebButtonText() != "Start web server" {
			t.Errorf("running=%v button=%q", h.a.web.Running(), h.a.win.WebButtonText())
		}
	})
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond); err == nil {
		t.Error("the port must be closed after stopping")
	}
}

func TestTrayStartsAndStopsTheServerToo(t *testing.T) {
	h, port := webHarness(t)
	withTray(h)
	onUI(func() {
		h.a.tray = ui.NewTray(ui.TrayActions{Open: h.a.win.Present, Quit: h.a.Quit, ToggleWeb: h.a.toggleWeb, OpenWeb: h.a.openWebPage})
		var opened []string
		h.a.openBrowser = func(url string) { opened = append(opened, url) }

		if h.a.tray.OpenWebAction().IsEnabled() {
			t.Error("there is no page to open yet")
		}
		h.a.tray.WebAction().Trigger()
		if !h.a.web.Running() || h.a.tray.WebAction().Text() != "Stop web server" || !h.a.tray.OpenWebAction().IsEnabled() {
			t.Errorf("running=%v item=%q", h.a.web.Running(), h.a.tray.WebAction().Text())
		}
		if h.a.win.WebButtonText() != "Stop web server" {
			t.Errorf("the window's button follows the tray: %q", h.a.win.WebButtonText())
		}
		h.a.tray.OpenWebAction().Trigger()
		if want := "http://localhost:" + strconv.Itoa(port) + "/"; len(opened) != 1 || opened[0] != want {
			t.Errorf("opened %v, want [%s]", opened, want)
		}
		h.a.tray.WebAction().Trigger()
		if h.a.web.Running() || h.a.tray.WebAction().Text() != "Start web server" || h.a.tray.OpenWebAction().IsEnabled() {
			t.Errorf("running=%v item=%q", h.a.web.Running(), h.a.tray.WebAction().Text())
		}
		h.a.openWebPage() // nothing to open
		if len(opened) != 1 {
			t.Errorf("a stopped server has no page: %v", opened)
		}
	})
}

func TestWebServerThatCannotStartShowsWhy(t *testing.T) {
	h, port := webHarness(t)
	taken, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	onUI(func() {
		h.a.win.WebButtonClick()
		if h.a.web.Running() || h.a.win.WebButtonText() != "Start web server" {
			t.Errorf("running=%v button=%q", h.a.web.Running(), h.a.win.WebButtonText())
		}
		if !strings.Contains(h.a.win.ErrorText(), "port "+strconv.Itoa(port)) || !strings.Contains(h.a.win.ErrorText(), "address already in use") {
			t.Errorf("banner = %q", h.a.win.ErrorText())
		}
	})
	if !strings.Contains(h.logs.String(), "WebServerStartFailed") {
		t.Errorf("the failure is logged:\n%s", h.logs.String())
	}
}

func TestQuitStopsTheWebServer(t *testing.T) {
	h, port := webHarness(t)
	quits := withTray(h)
	onUI(func() {
		h.a.StartWeb()
		h.a.Quit()
		if h.a.web.Running() {
			t.Error("quitting stops the web server")
		}
	})
	if *quits != 1 {
		t.Errorf("quit %d times", *quits)
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond); err == nil {
		t.Error("the port must be closed after quitting")
	}
}

func TestClosingTheWindowToTheTrayKeepsTheWebServer(t *testing.T) {
	h, port := webHarness(t)
	withTray(h)
	onUI(func() {
		h.a.Show()
		h.a.StartWeb()
		h.a.win.Close()
		if !h.a.web.Running() {
			t.Error("hiding the window must not stop the server")
		}
	})
	if code, _ := httpGet(t, port, "/"); code != http.StatusSeeOther {
		t.Errorf("GET / = %d", code)
	}
}

func TestSettingsUseTheStoredPortAndPassword(t *testing.T) {
	h, _ := webHarness(t)
	ctx := context.Background()
	p := preferences{store: h.st}
	port := freePort(t)
	if err := p.SetWebPort(ctx, port); err != nil {
		t.Fatal(err)
	}
	if err := p.SetWebPassword(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	hash, _ := h.st.WebPasswordHash(ctx)
	if hash == "" || strings.Contains(hash, "correct horse") || !web.CheckPassword(hash, "correct horse") {
		t.Errorf("the password is stored as a hash: %q", hash)
	}
	onUI(func() { h.a.StartWeb() })
	if got := h.a.web.Port(); got != port {
		t.Errorf("port = %d, want %d", got, port)
	}
}

func TestChangingTheSettingsRestartsARunningServer(t *testing.T) {
	h, port := webHarness(t)
	onUI(func() { h.a.StartWeb() })
	other := freePort(t)
	if err := h.st.SetWebPort(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	onUI(func() {
		h.a.onWebSettingsChanged()
		if !h.a.web.Running() || h.a.web.Port() != other {
			t.Errorf("running=%v port=%d, want %d", h.a.web.Running(), h.a.web.Port(), other)
		}
	})
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond); err == nil {
		t.Error("the old port must be closed")
	}
	// A stopped server is left alone.
	onUI(func() {
		h.a.StopWeb()
		h.a.onWebSettingsChanged()
		if h.a.web.Running() {
			t.Error("changing settings must not start a stopped server")
		}
	})
}

func TestPreferencesBackendWebSettings(t *testing.T) {
	ctx := context.Background()
	p := preferences{store: newStore(t)}
	got, err := p.WebSettings(ctx)
	if err != nil || got != (ui.WebSettings{Port: 3333}) {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if err := p.SetWebPort(ctx, 4100); err != nil {
		t.Fatal(err)
	}
	if err := p.SetWebPassword(ctx, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.WebSettings(ctx); got != (ui.WebSettings{Port: 4100, HasPassword: true}) {
		t.Errorf("settings = %+v", got)
	}
	if err := p.SetWebPassword(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.WebSettings(ctx); got.HasPassword {
		t.Error("an empty password removes it")
	}
	assertTag(t, p.SetWebPort(ctx, 0), "Store.SetWebPort->ValidateWebPort.WebPortInvalid")
	assertTag(t, p.SetWebPassword(ctx, "short"), "preferences.SetWebPassword->HashPassword.WebPasswordInvalid")
}

// A browser changes something that the window shows too: the window follows.
func TestWebPageChangesReachTheWindow(t *testing.T) {
	h, port := webHarness(t)
	second := addLLM(t, h.st, "second")
	onUI(func() { h.a.StartWeb() })
	base := "http://localhost:" + strconv.Itoa(port)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/w/") {
		t.Fatalf("Location = %q", loc)
	}
	req, err := http.NewRequest("POST", base+loc+"llm?id="+strconv.Itoa(int(second)), strings.NewReader(`{"text":""}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Origin", base)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST = %d", resp.StatusCode)
	}
	h.waitFor("the window to select the LLM chosen in the browser", func() bool { return h.a.win.SelectedLLMID() == second })
}
