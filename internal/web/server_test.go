package web

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

func TestHashPassword(t *testing.T) {
	h1, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("the same password must give different hashes (salt)")
	}
	if strings.Contains(h1, "correct horse") || !strings.HasPrefix(h1, "pbkdf2-sha256$600000$") {
		t.Errorf("hash = %q", h1)
	}
	if !CheckPassword(h1, "correct horse") || !CheckPassword(h2, "correct horse") {
		t.Error("the right password must match")
	}
	for _, wrong := range []string{"", "correct hors", "Correct horse", "correct horse "} {
		if CheckPassword(h1, wrong) {
			t.Errorf("%q must not match", wrong)
		}
	}
}

func TestHashPasswordRefusesWeakOnes(t *testing.T) {
	for _, pw := range []string{"", "short", "1234567", strings.Repeat("é", 7)} {
		_, err := HashPassword(pw)
		assertRoute(t, err, "HashPassword.WebPasswordInvalid")
	}
	_, err := HashPassword(strings.Repeat("a", maxPasswordBytes+1))
	assertRoute(t, err, "HashPassword.WebPasswordInvalid")
	if _, err := HashPassword("12345678"); err != nil {
		t.Errorf("8 characters are enough: %v", err)
	}
	if _, err := HashPassword("ééééééée"); err != nil {
		t.Errorf("characters, not bytes, are counted: %v", err)
	}
}

func TestCheckPasswordWithADamagedHash(t *testing.T) {
	good := quickHash(t, "secret-pass")
	for _, h := range []string{"", "x", "pbkdf2-sha256", "pbkdf2-sha256$a$b$c", "pbkdf2-sha256$0$AAAA$AAAA",
		"pbkdf2-sha256$99999999999$AAAA$AAAA", "md5$1000$AAAA$AAAA", "pbkdf2-sha256$1000$!!!$AAAA", "pbkdf2-sha256$1000$AAAA$"} {
		if CheckPassword(h, "secret-pass") {
			t.Errorf("CheckPassword(%q) = true", h)
		}
	}
	if !CheckPassword(good, "secret-pass") || CheckPassword(good, strings.Repeat("a", maxPasswordBytes+1)) {
		t.Error("CheckPassword with a good hash")
	}
}

func TestStartAndStop(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	if !e.srv.Running() || e.srv.Port() != e.port || e.srv.URL() != "http://localhost:"+strconv.Itoa(e.port)+"/" {
		t.Errorf("running=%v port=%d url=%q", e.srv.Running(), e.srv.Port(), e.srv.URL())
	}
	b := e.browser()
	b.open()

	e.srv.Stop()
	if e.srv.Running() || e.srv.Port() != 0 || e.srv.URL() != "" {
		t.Errorf("after Stop: running=%v port=%d url=%q", e.srv.Running(), e.srv.Port(), e.srv.URL())
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(e.port)), time.Second); err == nil {
		_ = conn.Close()
		t.Error("the port must be free after Stop")
	}
	e.srv.Stop() // stopping a stopped server is fine

	// It starts again, on another port, with no page left from before.
	port := freePort(t)
	if err := e.srv.Start(t.Context(), Options{Port: port}); err != nil {
		t.Fatal(err)
	}
	e.port = port
	e.browser().open()
	if n := len(e.workspaces()); n != 1 {
		t.Errorf("a restarted server starts with no pages, has %d", n)
	}
}

func TestStartFailures(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	assertRoute(t, e.srv.Start(t.Context(), Options{Port: freePort(t)}), "Server.Start.WebServerAlreadyRunning")

	other := New(context.Background(), Deps{Store: e.st, Stats: e.stats})
	err := other.Start(t.Context(), Options{Port: e.port})
	assertRoute(t, err, "Server.Start->listen->listenFailed.WebServerStartFailed")
	appErr, _ := errors.FromError(err)
	if !strings.Contains(appErr.Message, "port "+strconv.Itoa(e.port)) || !strings.Contains(appErr.Message, "address already in use") {
		t.Errorf("the message says why: %q", appErr.Message)
	}
	if other.Running() {
		t.Error("a server that failed to start is not running")
	}

	for _, port := range []int{0, -1, 65536} {
		assertRoute(t, other.Start(t.Context(), Options{Port: port}), "Server.Start->ValidateWebPort.WebPortInvalid")
	}
}

func TestStopEndsTheStreamsAndTheWorkThePagesStarted(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesReply(), Delay: time.Minute})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "toolbar", ">Cancel<")

	started := time.Now()
	e.srv.Stop()
	if d := time.Since(started); d > 3*time.Second {
		t.Errorf("Stop took %v with a check running", d)
	}
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Error("the stream must end when the server stops")
	}
}

func TestOnePageCannotUseAnothersWorkspace(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	a, b := e.browser(), e.browser()
	idA, _ := a.open()
	b.open() // b has a session too

	if code := b.post(idA, "text", "stolen"); code != 404 {
		t.Errorf("another browser's action = %d, want 404", code)
	}
	resp := b.do("GET", "/w/"+idA+"/events", nil, map[string]string{"Datastar-Request": "true"})
	_ = readBody(t, resp)
	if resp.StatusCode != 404 {
		t.Errorf("another browser's stream = %d, want 404", resp.StatusCode)
	}
	resp = b.do("GET", "/w/"+idA+"/", nil, nil)
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("another browser's page = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	ws := e.workspaces()
	for _, w := range ws {
		w.with(func() {
			if w.text == "stolen" {
				t.Error("the text of a page was changed by another browser")
			}
		})
	}
}

func TestActionsNeedTheDatastarHeaderAndAKnownPage(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()

	resp := b.do("POST", "/w/"+id+"/check", strings.NewReader("{}"), map[string]string{"Origin": e.base(), "Content-Type": "application/json"})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("without the header = %d, want 400", resp.StatusCode)
	}
	if code := b.post("nonsense", "check", ""); code != 404 {
		t.Errorf("an unknown page = %d, want 404", code)
	}
	resp = b.do("POST", "/w/"+id+"/text", strings.NewReader("{broken"), b.actionHeaders("application/json"))
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a broken body = %d, want 400", resp.StatusCode)
	}
	resp = b.do("GET", "/w/nonsense/", nil, nil)
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("a page that is gone = %d %q; want a way back to /", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestATextBeyondTheLimitIsIgnored(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	b.post(id, "text", "small")
	s := b.stream(id)
	b.post(id, "text", strings.Repeat("a", maxTextBytes+1))
	s.waitRegion(t, "banner", "longer than 10 MB")
	ws := e.workspaces()[0]
	ws.with(func() {
		if ws.text != "small" {
			t.Errorf("an over-long text was taken: %d bytes", len(ws.text))
		}
	})
}

func TestFormActionsNeedASameOriginPost(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	hdr := b.actionHeaders("application/x-www-form-urlencoded")
	hdr["Origin"] = "http://evil.example"
	resp := b.do("POST", "/w/"+id+"/modal/submit", strings.NewReader(url.Values{"value": {"x"}}.Encode()), hdr)
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site form = %d, want 403", resp.StatusCode)
	}
}

func TestPagesAreLimitedAndIdleOnesGoFirst(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333})
	now := tr.clock.t
	var first *workspace
	for i := range workspaceLimit {
		w := tr.srv.newWorkspace(tr.run, "owner")
		w.lastSeen = now.Add(time.Duration(i) * time.Second)
		if !tr.addWorkspace(w, now.Add(time.Hour)) {
			t.Fatalf("page %d refused", i)
		}
		if i == 0 {
			first = w
		}
	}
	extra := tr.srv.newWorkspace(tr.run, "owner")
	if !tr.addWorkspace(extra, now.Add(time.Hour)) {
		t.Fatal("a new page must replace the longest idle one")
	}
	if _, ok := tr.workspaceFor(first.id, "owner"); ok {
		t.Error("the longest idle page should have been dropped")
	}
	first.with(func() {
		if !first.closed {
			t.Error("a dropped page is closed")
		}
	})

	// Pages with a browser connected are never dropped.
	for _, w := range tr.workspaces {
		w.subs[&subscriber{wake: make(chan struct{}, 1), done: make(chan struct{})}] = struct{}{}
	}
	if tr.addWorkspace(tr.srv.newWorkspace(tr.run, "owner"), now.Add(time.Hour)) {
		t.Error("with every page in use there is no room")
	}
}

func TestIdlePagesAreSweptAway(t *testing.T) {
	tr := newTestRun(t, Options{Port: 3333})
	now := tr.clock.t
	old := tr.srv.newWorkspace(tr.run, "o")
	old.lastSeen = now.Add(-idleAfter - time.Minute)
	fresh := tr.srv.newWorkspace(tr.run, "o")
	fresh.lastSeen = now
	busy := tr.srv.newWorkspace(tr.run, "o")
	busy.lastSeen = now.Add(-idleAfter - time.Hour)
	busy.subs[&subscriber{wake: make(chan struct{}, 1), done: make(chan struct{})}] = struct{}{}
	for _, w := range []*workspace{old, fresh, busy} {
		tr.addWorkspace(w, now)
	}
	tr.sweep(now, idleAfter)
	if _, ok := tr.workspaceFor(old.id, "o"); ok {
		t.Error("an idle page is swept")
	}
	for _, w := range []*workspace{fresh, busy} {
		if _, ok := tr.workspaceFor(w.id, "o"); !ok {
			t.Error("a page in use is kept")
		}
	}
}

func TestDiffEdit(t *testing.T) {
	tests := []struct {
		name, a, b string
		want       issues.Edit
	}{
		{"same", "abc", "abc", issues.Edit{Pos: 3}},
		{"insert at the end", "abc", "abcd", issues.Edit{Pos: 3, Added: 1}},
		{"insert at the start", "abc", "xabc", issues.Edit{Pos: 0, Added: 1}},
		{"insert in the middle", "abc", "abxc", issues.Edit{Pos: 2, Added: 1}},
		{"delete", "abcd", "abd", issues.Edit{Pos: 2, Removed: 1}},
		{"replace", "abcd", "aXYd", issues.Edit{Pos: 1, Removed: 2, Added: 2}},
		{"everything", "abc", "", issues.Edit{Pos: 0, Removed: 3}},
		{"from nothing", "", "abc", issues.Edit{Pos: 0, Added: 3}},
		{"repeated characters", "aaa", "aaaa", issues.Edit{Pos: 3, Added: 1}},
		{"emoji count as two units", "a😀b", "a😀cb", issues.Edit{Pos: 3, Added: 1}},
		{"an emoji replaced by another", "a😀b", "a😁b", issues.Edit{Pos: 2, Removed: 1, Added: 1}},
		{"accents", "café", "cafés", issues.Edit{Pos: 4, Added: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffEdit(tt.a, tt.b); got != tt.want {
				t.Errorf("diffEdit(%q, %q) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func FuzzDiffEditRebuildsTheText(f *testing.F) {
	for _, s := range [][2]string{{"abc", "abd"}, {"", "x"}, {"😀", "😀😀"}, {"aaa", "aa"}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		e := diffEdit(a, b)
		ua, ub := utf16Of(a), utf16Of(b)
		if e.Pos < 0 || e.Removed < 0 || e.Added < 0 || e.Pos+e.Removed > len(ua) || e.Pos+e.Added > len(ub) {
			t.Fatalf("diffEdit(%q, %q) = %+v is out of range", a, b, e)
		}
		// Replacing the removed units of a by the added units of b gives b.
		got := append(append(append([]uint16{}, ua[:e.Pos]...), ub[e.Pos:e.Pos+e.Added]...), ua[e.Pos+e.Removed:]...)
		if len(got) != len(ub) {
			t.Fatalf("length %d, want %d", len(got), len(ub))
		}
		for i := range got {
			if got[i] != ub[i] {
				t.Fatalf("diffEdit(%q, %q) = %+v does not rebuild the text", a, b, e)
			}
		}
	})
}

func TestUnderlayIsEscapedAndUsesUTF16Offsets(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	b.open()
	w := e.workspaces()[0]
	text := "😀 <b>x</b> & He go"
	m := analysis.NewUTF16Map(text)
	start := strings.Index(text, "He go")
	w.with(func() {
		w.text = text
		w.state.FinishCheck(w.state.BeginCheck(), issues.FromPlaced(text, []analysis.Placed{{
			Issue: analysis.Issue{Text: "He go", Type: analysis.Resolve("verb_form"), Message: `say "goes" <now>`},
			Start: start, End: start + len("He go"),
		}}))
		got := string(w.renderUnder())
		want := `<div id="underlay" aria-hidden="true">😀 &lt;b&gt;x&lt;/b&gt; &amp; <span class="u u-grammar" data-tip="Verb form · Grammar` + "\n" +
			`say &#34;goes&#34; &lt;now&gt;">He go</span>` + "\u200b</div>"
		if got != want {
			t.Errorf("renderUnder() = %q\nwant %q", got, want)
		}
		if m.Offset(start) != 14 {
			t.Errorf("test setup: the issue should start at UTF-16 offset 14, got %d", m.Offset(start))
		}
		if it := w.state.Items()[0]; it.Start != 14 {
			t.Errorf("issue start = %d", it.Start)
		}
	})
}

func TestEditorTextNeverReachesTheLog(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesReply("He go", "verb_form"), Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", "He go to my secret diary.")
	s.waitRegion(t, "issues", "Issues (1)")
	e.srv.Stop()
	if log := e.logs.String(); strings.Contains(log, "secret diary") || strings.Contains(log, id) {
		t.Errorf("the log has the text or the key to the page:\n%s", log)
	}
}

func utf16Of(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}
