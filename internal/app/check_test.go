package app

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// harness drives an App from the test goroutine, which glutter's test binding
// takes as the UI thread. The results of background work are posted to it and
// run when the test calls onUI or settle.
type harness struct {
	t      *testing.T
	tester *gluttertest.Tester
	a      *App
	st     *store.Store
	logs   *syncBuffer
	llm    uint
}

// onUI runs f on the UI thread and then the callbacks that background work has
// posted so far.
func (h *harness) onUI(f func()) {
	f()
	h.tester.FlushPosted()
}

func newHarness(t *testing.T, fake *llmtest.Fake) *harness {
	t.Helper()
	return newHarnessLLM(t, fake, "local")
}

// newHarnessLLM is newHarness with a name for the one configured LLM.
func newHarnessLLM(t *testing.T, fake *llmtest.Fake, llmName string) *harness {
	t.Helper()
	st := newStore(t)
	h := &harness{t: t, st: st, logs: &syncBuffer{}, llm: addLLM(t, st, llmName), tester: gluttertest.New(t)}
	h.a = New(context.Background(), slog.New(slog.NewTextHandler(h.logs, nil)), st)
	h.a.newModel = func(store.LLMConfig) (llms.Model, error) { return fake, nil }
	h.tester.PumpWidget(h.a.Root())
	t.Cleanup(func() {
		// Let background work finish (its results are posted to the UI thread
		// first, and run before the window goes), then delete the window.
		h.a.analyzer.Cancel()
		h.a.explainer.Cancel()
		h.a.suggester.Cancel()
		h.settle()
		h.a.win.Dispose()
	})
	return h
}

// settle waits until no background work is left, running the callbacks that
// work posts to the UI thread: a callback can start more work (recording a check
// refreshes the counts), so it repeats until a pass finds nothing running.
func (h *harness) settle() {
	for range 20 {
		h.a.Wait()
		h.tester.FlushPosted() // runs the callbacks posted so far
		h.a.bgMu.Lock()
		idle := h.a.bgN == 0
		h.a.bgMu.Unlock()
		if idle {
			return
		}
	}
}

// sleep lets d pass in both the real and the fake clock, so that goroutines
// make progress and the timers of the application fire.
func (h *harness) sleep(d time.Duration) {
	h.t.Helper()
	const step = 5 * time.Millisecond
	for ; d > 0; d -= step {
		h.tester.Pump(step)
		h.tester.FlushPosted()
		time.Sleep(step)
	}
}

// pumpHistory lets the undo history of the editor close its current step.
func (h *harness) pumpHistory() {
	h.tester.Pump()
	h.tester.Pump(time.Second)
}

func (h *harness) setText(text string) {
	h.t.Helper()
	h.onUI(func() { h.a.win.SetText(text) })
}

func (h *harness) checkButton() {
	h.t.Helper()
	h.onUI(func() { h.a.win.CheckButtonClick() })
}

// waitFor polls cond on the UI thread until it holds.
func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		h.onUI(func() { ok = cond() })
		if ok {
			return
		}
		h.tester.Pump(2 * time.Millisecond) // the fake clock runs the timers
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) waitIdle() {
	h.t.Helper()
	h.waitFor("the check to end", func() bool { return !h.a.checking })
}

func (h *harness) underlines() (us []gui.Underline) {
	h.onUI(func() { us = h.a.win.Underlines() })
	return us
}

func (h *harness) statusText() (s string) {
	h.onUI(func() { s = h.a.win.StatusText() })
	return s
}

func (h *harness) events() (n int) {
	var rows []store.TypeStat
	var err error
	h.onUI(func() {})
	rows, err = h.a.stats.TypeStats(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	for _, r := range rows {
		n += r.Total
	}
	return n
}

func issuesReply(types map[string]string) string {
	var parts []string
	for text, typ := range types {
		parts = append(parts, `{"text":"`+text+`","error_type":"`+typ+`","message":"msg for `+text+`"}`)
	}
	return `{"issues":[` + strings.Join(parts, ",") + `]}`
}

var info = map[string]any{"PromptTokens": 120, "CompletionTokens": 30}

const sample = "He go home. She have a cat."

func twoIssues() llmtest.Reply {
	return llmtest.Reply{Text: issuesReply(map[string]string{"He go": "verb_form", "She have": "spelling"}), Info: info}
}

func TestManualCheckUnderlinesAndRecords(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.onUI(func() {
		if h.a.win.CheckButtonText() != "Cancel" || h.a.win.StatusText() != "Checking..." {
			t.Errorf("while checking: button %q, status %q", h.a.win.CheckButtonText(), h.a.win.StatusText())
		}
	})
	h.waitIdle()

	us := h.underlines()
	if len(us) != 2 {
		t.Fatalf("underlines = %+v", us)
	}
	if us[0].Start != 0 || us[0].End != 5 || us[0].Category != analysis.Grammar {
		t.Errorf("first underline = %+v", us[0])
	}
	if us[1].Start != 12 || us[1].End != 20 || us[1].Category != analysis.Vocabulary {
		t.Errorf("second underline = %+v", us[1])
	}
	h.onUI(func() {
		if got := h.a.win.StatusText(); got != "2 issues" {
			t.Errorf("status = %q", got)
		}
		if got := h.a.win.TokensText(); got != "last: 120 in / 30 out · session: 120 in / 30 out" {
			t.Errorf("tokens = %q", got)
		}
		if h.a.win.CheckButtonText() != "Check" || h.a.win.ErrorText() != "" {
			t.Errorf("button %q, error %q", h.a.win.CheckButtonText(), h.a.win.ErrorText())
		}
	})
	h.waitFor("the mistakes to be recorded", func() bool { return h.events() == 2 })
}

func TestAutoCheckResultIsNotRecorded(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) })
	h.waitIdle()
	if len(h.underlines()) != 2 {
		t.Fatalf("underlines = %+v", h.underlines())
	}
	h.sleep(50 * time.Millisecond) // an unwanted recording would be asynchronous
	if n := h.events(); n != 0 {
		t.Errorf("an auto-check recorded %d mistakes, want 0", n)
	}
}

func TestCheckingTheSameTextTwiceCountsOnce(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	for range 2 {
		h.checkButton()
		h.waitIdle()
	}
	h.waitFor("recording", func() bool { return h.events() >= 2 })
	h.sleep(50 * time.Millisecond)
	if n := h.events(); n != 2 {
		t.Errorf("events = %d, want 2 after checking twice", n)
	}
}

func TestMistakesAreRecordedUnderTheSessionSourceWithTheItemsFingerprint(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.waitFor("recording", func() bool { return h.events() == 2 })

	var key, fingerprint string
	h.onUI(func() {
		key = h.a.sourceKey
		fingerprint = h.a.state.Items()[0].Fingerprint
	})
	if !strings.HasPrefix(key, "session:") {
		t.Errorf("source key = %q", key)
	}
	// "Not an error" finds the event by (source, fingerprint): this only works if
	// both are what the recording used.
	if err := h.a.stats.NotAnError(context.Background(), key, fingerprint); err != nil {
		t.Fatal(err)
	}
	if n := h.events(); n != 1 {
		t.Errorf("events = %d, want 1 after removing the first issue's event", n)
	}
}

func TestCancelKeepsTheOldIssues(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: issuesReply(nil), Delay: 5 * time.Second})
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	before := h.underlines()

	h.checkButton() // second check: slow
	h.waitFor("the second request", func() bool { return len(fake.Calls()) == 2 })
	h.checkButton() // now the Cancel button
	h.waitIdle()

	h.onUI(func() {
		if got := h.a.win.StatusText(); got != "Check cancelled" {
			t.Errorf("status = %q", got)
		}
		if h.a.win.ErrorText() != "" || h.a.win.CheckButtonText() != "Check" {
			t.Errorf("error %q, button %q: cancelling is not an error", h.a.win.ErrorText(), h.a.win.CheckButtonText())
		}
	})
	if after := h.underlines(); len(after) != len(before) || after[0] != before[0] {
		t.Errorf("underlines changed: %+v -> %+v", before, after)
	}
	if strings.Contains(h.logs.String(), "level=ERROR") {
		t.Errorf("cancelling must not log an error:\n%s", h.logs.String())
	}
	h.waitFor("recording", func() bool { return h.events() == 2 })
	h.sleep(30 * time.Millisecond)
	if n := h.events(); n != 2 {
		t.Errorf("events = %d: a cancelled check records nothing", n)
	}
}

func TestFailedCheckShowsABannerAndKeepsTheIssues(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Err: io.ErrUnexpectedEOF}, twoIssues())
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.checkButton()
	h.waitIdle()

	h.onUI(func() {
		if got := h.a.win.StatusText(); got != "Check failed" {
			t.Errorf("status = %q", got)
		}
		if got := h.a.win.ErrorText(); !strings.Contains(got, "The request to local failed") {
			t.Errorf("banner = %q", got)
		}
	})
	if n := len(h.underlines()); n != 2 {
		t.Errorf("a failed check must keep the old underlines, got %d", n)
	}
	if out := h.logs.String(); !strings.Contains(out, "LLMCallFailed") || !strings.Contains(out, "level=ERROR") {
		t.Errorf("the error must be logged as JSON:\n%s", out)
	}

	// The next successful check clears the banner.
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		if h.a.win.ErrorText() != "" {
			t.Errorf("banner = %q, want it cleared by the next check", h.a.win.ErrorText())
		}
	})
}

func TestUnusableModelConfigurationFailsWithAMessage(t *testing.T) {
	h := newHarness(t, llmtest.New())
	h.onUI(func() {
		h.a.newModel = func(store.LLMConfig) (llms.Model, error) {
			return nil, errors.New("LLMClientCreateFailed", "The client for local could not be created.")
		}
		h.a.win.SetText(sample)
		h.a.startCheck(true)
		if h.a.checking || h.a.win.ErrorText() != "The client for local could not be created." || h.a.win.StatusText() != "Check failed" {
			t.Errorf("checking=%v banner=%q status=%q", h.a.checking, h.a.win.ErrorText(), h.a.win.StatusText())
		}
	})
}

func TestEditsDuringACheckAreAppliedToTheResult(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesReply(map[string]string{"He go": "verb_form", "She have": "spelling"}), Info: info, Delay: 100 * time.Millisecond})
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton()
	h.waitFor("the request", func() bool { return len(fake.Calls()) == 1 })
	h.onUI(func() { h.a.win.InsertTextAt(3, "XXXX") }) // inside "He go": the first issue is dropped
	h.waitIdle()
	us := h.underlines()
	if len(us) != 1 || us[0].Category != analysis.Vocabulary {
		t.Fatalf("underlines = %+v, want only the second issue", us)
	}
	h.onUI(func() {
		text := h.a.win.Text()
		start := analysis.NewUTF16Map(text).ByteOffset(us[0].Start)
		end := analysis.NewUTF16Map(text).ByteOffset(us[0].End)
		if got := text[start:end]; got != "She have" {
			t.Errorf("the second issue now covers %q in %q", got, text)
		}
	})
}

func TestEmptyTextAndNoLLMMakeNoRequest(t *testing.T) {
	fake := llmtest.New(twoIssues())
	h := newHarness(t, fake)
	h.onUI(func() {
		h.a.startCheck(true)
		if got := h.a.win.StatusText(); !strings.Contains(got, "Nothing to check") {
			t.Errorf("status = %q", got)
		}
		h.a.win.SetText("   \n ")
		h.a.startCheck(false) // auto-check stays silent
		h.a.win.SetText(sample)
		h.a.win.SetLLMs(nil, 0)
		h.a.startCheck(true)
	})
	h.sleep(30 * time.Millisecond)
	if n := len(fake.Calls()); n != 0 || h.a.checking {
		t.Errorf("calls = %d, checking = %v", n, h.a.checking)
	}
}

func TestNewerCheckSupersedesTheOlderOne(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: issuesReply(map[string]string{"He go": "verb_form"}), Delay: 5 * time.Second},
		llmtest.Reply{Text: issuesReply(map[string]string{"She have": "spelling"}), Info: info},
	)
	h := newHarness(t, fake)
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) })
	h.waitFor("the first request", func() bool { return len(fake.Calls()) == 1 })
	h.onUI(func() { h.a.startCheck(false) })
	h.waitIdle()
	h.sleep(50 * time.Millisecond) // the first check reports its cancellation meanwhile
	us := h.underlines()
	if len(us) != 1 || us[0].Category != analysis.Vocabulary {
		t.Errorf("underlines = %+v, want only the newer check's issue", us)
	}
	if got := h.statusText(); got != "1 issue" {
		t.Errorf("status = %q: the older check must not overwrite it", got)
	}
}

func TestSessionTotalsAccumulate(t *testing.T) {
	h := newHarness(t, llmtest.New(
		llmtest.Reply{Text: issuesReply(nil), Info: map[string]any{"PromptTokens": 100, "CompletionTokens": 10}},
		llmtest.Reply{Text: issuesReply(nil), Info: map[string]any{"PromptTokens": 50, "CompletionTokens": 5}},
	))
	h.setText(sample)
	for range 2 {
		h.checkButton()
		h.waitIdle()
	}
	h.onUI(func() {
		if got := h.a.win.TokensText(); got != "last: 50 in / 5 out · session: 150 in / 15 out" {
			t.Errorf("tokens = %q", got)
		}
		if got := h.a.win.StatusText(); got != "No errors found" {
			t.Errorf("status = %q", got)
		}
	})
}

func TestSelectionFromTheTextAndFromTheState(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		// A click inside the second issue selects it and leaves the cursor where it is.
		h.a.win.MoveCursorTo(15)
		sel, ok := h.a.state.Selected()
		if !ok || sel.Category() != analysis.Vocabulary || h.a.win.CursorPosition() != 15 {
			t.Errorf("selected %+v (%v), cursor %d", sel, ok, h.a.win.CursorPosition())
		}
		if us := h.a.win.Underlines(); !us[1].Selected || us[0].Selected {
			t.Errorf("underlines = %+v: only the second should be drawn selected", us)
		}
		// The cursor leaving the issue clears the selection.
		h.a.win.MoveCursorTo(10)
		if _, ok := h.a.state.Selected(); ok {
			t.Error("the cursor left the issue, so nothing should be selected")
		}
		// Selecting from the state (the list does this) moves the cursor to the start.
		first := h.a.state.Items()[0]
		h.a.state.Select(first.ID)
		if h.a.win.CursorPosition() != first.Start {
			t.Errorf("cursor = %d, want the start of the issue (%d)", h.a.win.CursorPosition(), first.Start)
		}
	})
}

func TestNextAndPreviousIssue(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		h.a.win.MoveCursorTo(7) // between the issues
		h.a.stepIssue(h.a.state.Next)
		if sel, _ := h.a.state.Selected(); sel.Start != 12 || h.a.win.CursorPosition() != 12 {
			t.Errorf("F7 from 7: selected %+v, cursor %d", sel, h.a.win.CursorPosition())
		}
		h.a.stepIssue(h.a.state.Next) // wraps to the first
		if sel, _ := h.a.state.Selected(); sel.Start != 0 || h.a.win.CursorPosition() != 0 {
			t.Errorf("F7 wraps: selected %+v, cursor %d", sel, h.a.win.CursorPosition())
		}
		h.a.stepIssue(h.a.state.Prev) // back to the last
		if sel, _ := h.a.state.Selected(); sel.Start != 12 {
			t.Errorf("Shift+F7: selected %+v", sel)
		}
	})
}

func TestLegendHidesUnderlines(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		h.a.win.LegendButtonClick(analysis.Vocabulary)
		us := h.a.win.Underlines()
		if len(us) != 1 || us[0].Category != analysis.Grammar {
			t.Errorf("underlines = %+v, want only grammar", us)
		}
		h.a.win.LegendButtonClick(analysis.Vocabulary)
		if len(h.a.win.Underlines()) != 2 {
			t.Error("showing the category again restores its underlines")
		}
	})
}

func TestTooltip(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		want := "Verb form · Grammar\nmsg for He go"
		if got := h.a.tooltipAt(2); got != want {
			t.Errorf("tooltip = %q, want %q", got, want)
		}
		if got := h.a.tooltipAt(8); got != "" {
			t.Errorf("tooltip between issues = %q, want none", got)
		}
		h.a.win.LegendButtonClick(analysis.Grammar)
		if got := h.a.tooltipAt(2); got != "" {
			t.Errorf("a hidden category has no tooltip, got %q", got)
		}
	})
}

func TestEditingRemovesTheIssueAndUpdatesTheStatus(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		h.a.win.InsertTextAt(2, "x") // inside "He go"
		if us := h.a.win.Underlines(); len(us) != 1 {
			t.Errorf("underlines = %+v", us)
		}
		if got := h.a.win.StatusText(); got != "1 issue" {
			t.Errorf("status = %q", got)
		}
	})
}

func TestUnplacedIssuesAreCounted(t *testing.T) {
	reply := `{"issues":[{"text":"He go","error_type":"verb_form"},{"text":"not in the text","error_type":"article"}]}`
	h := newHarness(t, llmtest.New(llmtest.Reply{Text: reply}))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	if got := h.statusText(); got != "1 issue · 1 issue could not be placed" {
		t.Errorf("status = %q", got)
	}
}

func TestSessionKeys(t *testing.T) {
	re := regexp.MustCompile(`^session:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for range 50 {
		k := newSessionKey()
		if !re.MatchString(k) {
			t.Fatalf("key = %q", k)
		}
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestAutoCheckWaitsForAPause(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesReply(map[string]string{"He go": "verb_form"}), Info: info})
	h := newHarness(t, fake)
	h.onUI(func() {
		h.a.autoDelay = 80 * time.Millisecond
		h.a.onAutoCheckChanged(true)
	})
	// Typing every 40 ms keeps pushing the check back.
	for i := range 4 {
		h.onUI(func() { h.a.win.SetText(sample + strings.Repeat("!", i+1)) })
		h.sleep(40 * time.Millisecond)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Fatalf("a check ran while the user was still typing (%d calls)", n)
	}
	h.waitFor("the auto-check", func() bool { return len(fake.Calls()) == 1 })
	h.waitIdle()
	h.sleep(150 * time.Millisecond)
	if n := len(fake.Calls()); n != 1 {
		t.Errorf("calls = %d, want exactly one auto-check", n)
	}
	if len(h.underlines()) != 1 {
		t.Errorf("underlines = %+v", h.underlines())
	}
	if n := h.events(); n != 0 {
		t.Errorf("auto-check recorded %d mistakes", n)
	}
}

func TestAutoCheckIsOffByDefault(t *testing.T) {
	fake := llmtest.New(twoIssues())
	h := newHarness(t, fake)
	h.onUI(func() {
		h.a.autoDelay = 10 * time.Millisecond
		h.a.win.SetText(sample)
	})
	h.sleep(100 * time.Millisecond)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("calls = %d: with auto-check off only the Check button starts a check", n)
	}
}

func TestTurningAutoCheckOffStopsAPendingOne(t *testing.T) {
	fake := llmtest.New(twoIssues())
	h := newHarness(t, fake)
	h.onUI(func() {
		h.a.autoDelay = 60 * time.Millisecond
		h.a.onAutoCheckChanged(true)
		h.a.win.SetText(sample)
		h.a.onAutoCheckChanged(false)
	})
	h.sleep(150 * time.Millisecond)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("calls = %d, want 0 after turning auto-check off", n)
	}
}

func TestAutoCheckWaitsForARunningCheck(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: issuesReply(nil), Delay: 250 * time.Millisecond},
		llmtest.Reply{Text: issuesReply(nil)},
	)
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton() // manual check, slow
	h.waitFor("the request", func() bool { return len(fake.Calls()) == 1 })
	h.onUI(func() {
		h.a.autoDelay = 40 * time.Millisecond
		h.a.onAutoCheckChanged(true)
		h.a.win.SetText(sample + " more")
	})
	h.sleep(120 * time.Millisecond)
	if n := len(fake.Calls()); n != 1 {
		t.Errorf("calls = %d: the auto-check must not cancel the running manual check", n)
	}
	h.waitFor("the auto-check after the manual one", func() bool { return len(fake.Calls()) == 2 })
}

func TestStateObserverIsWired(t *testing.T) {
	h := newHarness(t, llmtest.New())
	h.onUI(func() {
		h.a.win.SetText(sample)
		rev := h.a.state.BeginCheck()
		h.a.state.FinishCheck(rev, []issues.Item{{Start: 0, End: 5, Issue: analysis.Issue{Text: "He go", Type: analysis.Resolve("article")}}})
		if len(h.a.win.Underlines()) != 1 {
			t.Error("a change of the state must redraw the underlines")
		}
	})
}
