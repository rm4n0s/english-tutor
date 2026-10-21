package app

import (
	"context"
	"github.com/rm4n0s/english-tutor/internal/view"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

func explainReply() llmtest.Reply {
	return llmtest.Reply{Chunks: []string{"### What is wrong\n", "Use *goes*.\n\n", "### Why\nThird person singular."}, Info: map[string]any{"PromptTokens": 40, "CompletionTokens": 20}}
}

// checked runs a manual check on the sample text with the fake's first reply
// and waits until it is finished and recorded.
func (h *harness) checked(fake *llmtest.Fake) {
	h.t.Helper()
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.waitFor("the mistakes to be recorded", func() bool { return h.events() >= 2 })
	h.waitFor("the badges", func() bool { return len(h.a.typeCounts) > 0 })
}

func (h *harness) selectIssue(id int) {
	h.t.Helper()
	h.onUI(func() { h.a.state.Select(id) })
}

func (h *harness) panel() (st view.ExplainState, header, body string) {
	h.onUI(func() {
		st, header, body = h.a.win.ExplanationState(), h.a.win.ExplanationHeader(), h.a.win.ExplanationText()
	})
	return st, header, body
}

func (h *harness) waitPanel(want view.ExplainState) {
	h.t.Helper()
	h.waitFor("the panel state", func() bool { return h.a.win.ExplanationState() == want })
}

func checkThenExplain() *llmtest.Fake {
	return llmtest.New(twoIssues(), explainReply())
}

func TestListShowsRowsHeaderAndBadges(t *testing.T) {
	h := newHarness(t, checkThenExplain())
	h.onUI(func() {
		if h.a.win.IssuesEmptyText() != "Press Check to find errors" || h.a.win.IssuesHeader() != "Issues (0)" {
			t.Errorf("before the first check: %q, %q", h.a.win.IssuesEmptyText(), h.a.win.IssuesHeader())
		}
	})
	h.checked(nil)
	h.onUI(func() {
		if h.a.win.IssuesHeader() != "Issues (2)" || !slices.Equal(h.a.win.IssueRowIDs(), []int{1, 2}) {
			t.Fatalf("header %q rows %v", h.a.win.IssuesHeader(), h.a.win.IssueRowIDs())
		}
		first := h.a.win.IssueRowText(0)
		if !strings.Contains(first, "He go") || !strings.Contains(first, ">G<") || !strings.Contains(first, "Verb form") || !strings.Contains(first, "<b>x1</b>") {
			t.Errorf("first row:\n%s", first)
		}
		if second := h.a.win.IssueRowText(1); !strings.Contains(second, ">V<") || !strings.Contains(second, "Spelling") {
			t.Errorf("second row:\n%s", second)
		}
	})
}

func TestBadgesAreHiddenForAnAutoCheck(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues()))
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) })
	h.waitIdle()
	h.onUI(func() {
		if strings.Contains(h.a.win.IssueRowText(0), "<b>x") {
			t.Errorf("an auto-check issue is not recorded, so it has no count badge:\n%s", h.a.win.IssueRowText(0))
		}
	})
}

func TestListEmptyStates(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: issuesReply(nil), Delay: 150 * time.Millisecond},
	)
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton()
	h.onUI(func() {
		if got := h.a.win.IssuesEmptyText(); got != "Checking..." {
			t.Errorf("while checking: %q", got)
		}
	})
	h.waitIdle()
	h.onUI(func() {
		if got := h.a.win.IssuesEmptyText(); got != "No errors found" {
			t.Errorf("after a check without issues: %q", got)
		}
	})
}

func TestSelectingAnIssueExplainsIt(t *testing.T) {
	slow := explainReply()
	slow.Delay = 200 * time.Millisecond // long enough to look at the loading state
	fake := llmtest.New(twoIssues(), slow)
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)

	h.onUI(func() {
		st, hdr, _ := h.a.win.ExplanationState(), h.a.win.ExplanationHeader(), ""
		if st != view.ExplainLoading || hdr == "" {
			t.Errorf("right after selecting: state %v, header %q", st, hdr)
		}
		if !h.a.win.ExplanationProgressShown() {
			t.Error("the busy indicator should show while loading")
		}
	})
	h.waitPanel(view.ExplainReady)
	st, header, body := h.panel()
	if st != view.ExplainReady || !strings.Contains(body, "Use goes.") || !strings.Contains(body, "Third person singular.") {
		t.Errorf("state %v, body %q", st, body)
	}
	h.waitFor("the header counts", func() bool { return strings.Contains(h.a.win.ExplanationHeader(), "you have made this mistake 1 time") })
	_ = header
	h.onUI(func() {
		if got := h.a.win.ExplanationHeader(); got != "Verb form: you have made this mistake 1 time (1 in the last 7 days)" {
			t.Errorf("header = %q", got)
		}
		redo, cancel, notAnError := h.a.win.ExplanationButtons()
		if redo != "Regenerate" || cancel || !notAnError {
			t.Errorf("buttons: %q %v %v", redo, cancel, notAnError)
		}
		if got := h.a.win.TokensText(); !strings.Contains(got, "last: 40 in / 20 out") {
			t.Errorf("tokens = %q: the explanation's usage should show", got)
		}
	})

	// The request carries the faulty words and the text around them.
	calls := fake.Calls()
	if len(calls) != 2 || !strings.Contains(calls[1].Prompt, "\nHe go\n") || !strings.Contains(calls[1].Prompt, sample) {
		t.Errorf("explain call = %+v", calls)
	}
}

func TestExplanationIsCachedUntilRegenerated(t *testing.T) {
	fake := llmtest.New(twoIssues(), explainReply(), llmtest.Reply{Text: "Regenerated explanation."})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainReady)

	h.onUI(func() {
		h.a.state.ClearSelection()
		if h.a.win.ExplanationState() != view.ExplainIdle {
			t.Error("clearing the selection returns the panel to idle")
		}
		h.a.state.Select(1)
	})
	h.waitPanel(view.ExplainReady)
	if n := len(fake.Calls()); n != 2 {
		t.Errorf("calls = %d: re-selecting must show the cached explanation", n)
	}

	h.onUI(func() { h.a.win.ClickExplainButton("redo") })
	h.waitFor("the regenerated text", func() bool { return strings.Contains(h.a.win.ExplanationText(), "Regenerated explanation.") })
	if n := len(fake.Calls()); n != 3 {
		t.Errorf("calls = %d, want a new request for Regenerate", n)
	}
	h.waitPanel(view.ExplainReady)
}

func TestExplanationFailureAndRetry(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Err: io.ErrUnexpectedEOF}, llmtest.Reply{Text: "Second try works."})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainFailed)
	st, _, body := h.panel()
	if st != view.ExplainFailed || !strings.Contains(body, "The request to local failed") {
		t.Errorf("state %v, body %q", st, body)
	}
	h.onUI(func() {
		if redo, _, _ := h.a.win.ExplanationButtons(); redo != "Retry" {
			t.Errorf("the failed panel offers %q", redo)
		}
	})
	if !strings.Contains(h.logs.String(), "explanation failed") || !strings.Contains(h.logs.String(), "LLMCallFailed") {
		t.Errorf("the failure must be logged:\n%s", h.logs.String())
	}

	h.onUI(func() { h.a.win.ClickExplainButton("redo") })
	h.waitPanel(view.ExplainReady)
	if _, _, body := h.panel(); !strings.Contains(body, "Second try works.") {
		t.Errorf("body = %q", body)
	}
}

func TestCancellingAnExplanation(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Chunks: []string{"partial"}, Delay: 5 * time.Second})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitFor("the explanation request", func() bool { return len(fake.Calls()) == 2 })
	h.onUI(func() {
		if _, cancel, _ := h.a.win.ExplanationButtons(); !cancel {
			t.Error("a loading explanation has a Cancel button")
		}
		h.a.win.ClickExplainButton("cancel")
	})
	h.waitPanel(view.ExplainCancelled)
	h.onUI(func() {
		redo, cancel, notAnError := h.a.win.ExplanationButtons()
		if redo != "Regenerate" || cancel || !notAnError {
			t.Errorf("buttons after cancelling: %q %v %v", redo, cancel, notAnError)
		}
		if h.a.win.ErrorText() != "" {
			t.Errorf("cancelling is not an error, banner = %q", h.a.win.ErrorText())
		}
	})
	if strings.Contains(h.logs.String(), "level=ERROR") {
		t.Errorf("cancelling must not log an error:\n%s", h.logs.String())
	}
}

func TestSelectingAnotherIssueReplacesTheExplanation(t *testing.T) {
	fake := llmtest.New(twoIssues(),
		llmtest.Reply{Text: "slow first", Delay: 5 * time.Second},
		llmtest.Reply{Text: "explanation of the second issue"},
	)
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitFor("the first request", func() bool { return len(fake.Calls()) == 2 })
	h.selectIssue(2)
	h.waitPanel(view.ExplainReady)
	h.sleep(50 * time.Millisecond) // the first one reports its cancellation meanwhile
	st, _, body := h.panel()
	if st != view.ExplainReady || !strings.Contains(body, "explanation of the second issue") {
		t.Errorf("state %v, body %q: the older explanation must not overwrite the newer one", st, body)
	}
}

func TestEditingTheSelectedIssueResetsThePanel(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "slow", Delay: 5 * time.Second})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitFor("the request", func() bool { return len(fake.Calls()) == 2 })
	h.onUI(func() { h.a.win.InsertTextAt(2, "x") }) // inside the first issue
	h.waitPanel(view.ExplainIdle)
	h.onUI(func() {
		if !slices.Equal(h.a.win.IssueRowIDs(), []int{2}) {
			t.Errorf("rows = %v: the row of an edited issue disappears at once", h.a.win.IssueRowIDs())
		}
	})
}

func TestExplainWithoutAnLLMFailsWithAMessage(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.SetLLMs(nil, 0)
		h.a.state.Select(1)
		if h.a.win.ExplanationState() != view.ExplainFailed {
			t.Errorf("state = %v", h.a.win.ExplanationState())
		}
	})
}

func TestListSelectionMovesTheCursorAndStartsTheExplanation(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.ClickIssueRow(1) // the second issue
		if sel, ok := h.a.state.Selected(); !ok || sel.ID != 2 {
			t.Errorf("selected %+v, %v", sel, ok)
		}
		if h.a.win.CursorPosition() != 12 || h.a.win.SelectedIssueRow() != 2 {
			t.Errorf("cursor %d, selected row %d", h.a.win.CursorPosition(), h.a.win.SelectedIssueRow())
		}
		if h.a.win.ExplanationState() != view.ExplainLoading {
			t.Errorf("state = %v", h.a.win.ExplanationState())
		}
		us := h.a.win.Underlines()
		if !us[1].Selected || us[0].Selected {
			t.Errorf("underlines = %+v", us)
		}
	})
	h.waitPanel(view.ExplainReady)
}

func TestSelectingInTheTextSelectsTheRow(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.MoveCursorTo(3)
		if h.a.win.SelectedIssueRow() != 1 || h.a.win.CursorPosition() != 3 {
			t.Errorf("selected row %d, cursor %d", h.a.win.SelectedIssueRow(), h.a.win.CursorPosition())
		}
		h.a.win.MoveCursorTo(9)
		if h.a.win.SelectedIssueRow() != 0 {
			t.Error("moving out of the issue clears the row selection")
		}
	})
	h.waitPanel(view.ExplainIdle)
}

func TestDoubleClickMovesTheKeyboardToTheEditor(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.MoveCursorTo(25)
		h.a.win.DoubleClickIssueRow(1)
		if h.a.win.CursorPosition() != 12 {
			t.Errorf("cursor = %d, want the start of the issue", h.a.win.CursorPosition())
		}
	})
}

func TestNotAnErrorRemovesTheIssueAndItsCount(t *testing.T) {
	fake := llmtest.New(twoIssues(), explainReply())
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainReady)

	h.onUI(func() { h.a.win.ClickExplainButton("notanerror") })
	h.onUI(func() {
		if !slices.Equal(h.a.win.IssueRowIDs(), []int{2}) || len(h.a.win.Underlines()) != 1 {
			t.Errorf("rows %v, underlines %+v", h.a.win.IssueRowIDs(), h.a.win.Underlines())
		}
		if h.a.win.ExplanationState() != view.ExplainIdle {
			t.Errorf("panel state = %v", h.a.win.ExplanationState())
		}
	})
	h.waitFor("the event to be deleted", func() bool { return h.events() == 1 })

	// A re-check must not bring it back, but still records the other issue once.
	fake2 := llmtest.New(twoIssues())
	h.onUI(func() { h.a.newModel = func(store.LLMConfig) (llms.Model, error) { return fake2, nil } })
	h.checkButton()
	h.waitIdle()
	h.sleep(60 * time.Millisecond)
	if n := h.events(); n != 1 {
		t.Errorf("events = %d, want 1: the ignored mistake stays out", n)
	}
	h.onUI(func() {
		if got := len(h.a.win.IssueRowIDs()); got != 2 {
			t.Errorf("the issue is still underlined after a re-check (the LLM said so again), rows = %d", got)
		}
	})
}

func TestNotAnErrorFromTheContextMenu(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.ClickIssueRow(0)
		h.a.win.TriggerIssueMenu("Not an error")
		if !slices.Equal(h.a.win.IssueRowIDs(), []int{2}) {
			t.Errorf("rows = %v", h.a.win.IssueRowIDs())
		}
	})
	h.waitFor("the event to be deleted", func() bool { return h.events() == 1 })
}

func TestRegenerateFromTheContextMenu(t *testing.T) {
	fake := llmtest.New(twoIssues(), explainReply(), llmtest.Reply{Text: "again"})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainReady)
	h.onUI(func() { h.a.win.TriggerIssueMenu("Regenerate explanation") })
	h.waitFor("the new explanation", func() bool { return strings.Contains(h.a.win.ExplanationText(), "again") })
}

func TestLegendFiltersTheList(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		h.a.win.LegendButtonClick(analysis.Vocabulary)
		if h.a.win.IssuesHeader() != "Issues (1 of 2 shown)" || !slices.Equal(h.a.win.IssueRowIDs(), []int{1}) {
			t.Errorf("header %q rows %v", h.a.win.IssuesHeader(), h.a.win.IssueRowIDs())
		}
		h.a.win.LegendButtonClick(analysis.Grammar)
		if got := h.a.win.IssuesEmptyText(); got != "All categories are hidden" {
			t.Errorf("empty text = %q", got)
		}
		h.a.win.LegendButtonClick(analysis.Grammar)
		h.a.win.LegendButtonClick(analysis.Vocabulary)
		if h.a.win.IssuesHeader() != "Issues (2)" {
			t.Errorf("header = %q", h.a.win.IssuesHeader())
		}
	})
}

func TestStreamingRendersThrottled(t *testing.T) {
	var chunks []string
	for i := range 40 {
		chunks = append(chunks, "word"+strings.Repeat("s", i%3)+" ")
	}
	fake := llmtest.New(twoIssues(), llmtest.Reply{Chunks: chunks})
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainReady)
	_, _, body := h.panel()
	if want := strings.TrimSpace(strings.Join(chunks, "")); !strings.Contains(strings.Join(strings.Fields(body), " "), want[:20]) {
		t.Errorf("body = %q", body)
	}
}

func TestNewResultClearsSelectionAndExplanation(t *testing.T) {
	fake := llmtest.New(twoIssues(), explainReply(), twoIssues())
	h := newHarness(t, fake)
	h.checked(fake)
	h.selectIssue(1)
	h.waitPanel(view.ExplainReady)
	h.checkButton()
	h.waitIdle()
	h.onUI(func() {
		if h.a.win.ExplanationState() != view.ExplainIdle || h.a.win.SelectedIssueRow() != 0 {
			t.Errorf("state %v, selected row %d: a new result clears the selection", h.a.win.ExplanationState(), h.a.win.SelectedIssueRow())
		}
	})
}

func TestUnrecordedIssueHeaderSaysItIsNotCounted(t *testing.T) {
	fake := llmtest.New(twoIssues(), explainReply())
	h := newHarness(t, fake)
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) })
	h.waitIdle()
	h.selectIssue(1)
	h.waitFor("the header", func() bool { return strings.Contains(h.a.win.ExplanationHeader(), "not counted") })
}

func TestResettingStatisticsClearsTheBadges(t *testing.T) {
	fake := checkThenExplain()
	h := newHarness(t, fake)
	h.checked(fake)
	h.onUI(func() {
		if !strings.Contains(h.a.win.IssueRowText(0), "<b>x1</b>") {
			t.Fatalf("expected a badge before the reset:\n%s", h.a.win.IssueRowText(0))
		}
	})
	if err := h.a.stats.ResetMistakes(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.onUI(func() {
		h.a.onStatisticsReset()
		if strings.Contains(h.a.win.IssueRowText(0), "<b>x") {
			t.Errorf("the badge must go after the reset:\n%s", h.a.win.IssueRowText(0))
		}
	})
}
