package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

func altReply(alts ...string) llmtest.Reply {
	quoted := make([]string, len(alts))
	for i, a := range alts {
		quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(a) + `"`
	}
	return llmtest.Reply{Text: `{"alternatives": [` + strings.Join(quoted, ",") + `]}`, Info: info}
}

// askAlternatives selects the UTF-16 range and chooses Alternative from the
// editor's right-click menu, as the user would.
func (h *harness) askAlternatives(start, end int) {
	h.t.Helper()
	onUI(func() {
		h.a.win.SelectRange(start, end)
		if !h.a.win.TriggerEditorMenu("Alternative") {
			h.t.Error("the menu has no Alternative entry")
		}
	})
}

func (h *harness) altDialog() (d *ui.AlternativesDialog) {
	onUI(func() { d = h.a.altDlg })
	return d
}

// waitAlternatives waits until the window shows alternatives (or a failure).
func (h *harness) waitAlternatives() {
	h.t.Helper()
	h.waitFor("the alternatives", func() bool { return h.a.altDlg != nil && !h.a.altDlg.Loading() })
}

func TestAlternativeFromTheContextMenu(t *testing.T) {
	fake := llmtest.New(altReply("huge", "large", "a big one"))
	h := newHarness(t, fake)
	h.setText(sample)
	h.askAlternatives(3, 5) // "go"

	onUI(func() {
		d := h.a.altDlg
		if d == nil || !d.IsVisible() {
			t.Fatal("the Alternatives window should be open")
		}
		if !d.Loading() || d.SelectionText() != "go" {
			t.Errorf("loading %v, selection %q", d.Loading(), d.SelectionText())
		}
	})
	h.waitAlternatives()
	onUI(func() {
		d := h.a.altDlg
		if !slices.Equal(d.Alternatives(), []string{"huge", "large", "a big one"}) || d.StatusText() != "3 alternatives" || d.ErrorText() != "" {
			t.Errorf("alternatives %q, status %q, error %q", d.Alternatives(), d.StatusText(), d.ErrorText())
		}
		if got := h.a.win.TokensText(); got != "last: 120 in / 30 out · session: 120 in / 30 out" {
			t.Errorf("the tokens are counted in the status bar: %q", got)
		}
		if h.a.win.Text() != sample || h.a.win.Modified() {
			t.Error("asking for alternatives must not change the text")
		}
	})
	calls := fake.Calls()
	if len(calls) != 1 || !strings.Contains(calls[0].Prompt, "\ngo\n=====END SELECTED TEXT ") || !strings.Contains(calls[0].Prompt, sample) {
		t.Fatalf("calls = %d, prompt:\n%s", len(calls), calls[0].Prompt)
	}
	h.settle()
	totals, err := h.a.stats.UsageTotals(h.a.ctx, store.AllTime)
	if err != nil || len(totals) != 1 || totals[0].InputTokens != 120 || totals[0].OutputTokens != 30 {
		t.Errorf("usage totals = %+v, %v", totals, err)
	}
}

func TestAlternativeSelections(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		start, end int // UTF-16 units
		want       string
	}{
		{"a word", "He go home.", 3, 5, "go"},
		{"a sentence", "He go home. She have a cat.", 12, 27, "She have a cat."},
		{"the whole text", "He go home.\n\nShe have a cat.", 0, 28, "He go home.\n\nShe have a cat."},
		{"after an emoji", "😀😀 He go home.", 8, 10, "go"},
		{"accented text", "Ça va très bien, merci.", 3, 5, "va"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(altReply("other words"))
			h := newHarness(t, fake)
			h.setText(tt.text)
			h.askAlternatives(tt.start, tt.end)
			h.waitAlternatives()
			onUI(func() {
				if got := h.a.altDlg.SelectionText(); got != tt.want {
					t.Errorf("the window shows %q, want %q", got, tt.want)
				}
			})
			if p := fake.Calls()[0].Prompt; !strings.Contains(p, "\n"+tt.want+"\n=====END SELECTED TEXT ") {
				t.Errorf("the model was sent the wrong selection:\n%s", p)
			}
		})
	}
}

func TestAlternativeNeedsASelection(t *testing.T) {
	fake := llmtest.New(altReply("x"))
	h := newHarness(t, fake)
	h.setText(sample)
	onUI(func() {
		h.a.win.SelectRange(3, 3)
		if h.a.win.TriggerEditorMenu("Alternative") {
			t.Error("Alternative is offered without a selection")
		}
		h.a.win.SelectRange(2, 3) // a space
		if h.a.win.TriggerEditorMenu("Alternative") {
			t.Error("Alternative is offered for a selection of only white space")
		}
		if h.a.altDlg != nil {
			t.Error("no window should be open")
		}
	})
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("calls = %d", n)
	}
}

func TestAlternativeFailureThenRetry(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Err: errors.New("connection refused")}, altReply("huge"))
	h := newHarness(t, fake)
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitAlternatives()
	onUI(func() {
		d := h.a.altDlg
		if !strings.Contains(d.ErrorText(), "connection refused") || !d.RetryVisible() || len(d.Alternatives()) != 0 {
			t.Errorf("error %q, retry %v", d.ErrorText(), d.RetryVisible())
		}
		if h.a.win.ErrorText() != "" {
			t.Errorf("the failure belongs in the window, not the banner: %q", h.a.win.ErrorText())
		}
		d.RetryClick()
		if !d.Loading() || d.ErrorText() != "" {
			t.Error("Retry should show the loading state again")
		}
	})
	h.waitAlternatives()
	onUI(func() {
		if got := h.a.altDlg.Alternatives(); !slices.Equal(got, []string{"huge"}) {
			t.Errorf("after Retry: %q", got)
		}
	})
	if !strings.Contains(h.logs.String(), "alternatives failed") {
		t.Errorf("the failure should be logged:\n%s", h.logs.String())
	}
}

func TestAlternativeInvalidReplyFailsWithItsMessage(t *testing.T) {
	h := newHarness(t, llmtest.New(llmtest.Reply{Text: "no json here"}))
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitAlternatives()
	onUI(func() {
		if got := h.a.altDlg.ErrorText(); got != "The model's answer was not in the expected format." {
			t.Errorf("error = %q", got)
		}
	})
}

func TestAlternativeSelectionTooLong(t *testing.T) {
	long := strings.Repeat("word ", 700)
	fake := llmtest.New(altReply("x"))
	h := newHarness(t, fake)
	h.setText(long)
	h.askAlternatives(0, len(long))
	h.waitAlternatives()
	onUI(func() {
		if got := h.a.altDlg.ErrorText(); !strings.Contains(got, "select at most 600") {
			t.Errorf("error = %q", got)
		}
	})
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("a selection that is too long must not reach the model, calls = %d", n)
	}
}

func TestAlternativeWithoutAnLLM(t *testing.T) {
	st := newStore(t) // no LLM configured
	withApp(t, st, func(a *App, logs *syncBuffer) {
		a.win.SetText(sample)
		a.win.SelectRange(3, 5)
		a.win.TriggerEditorMenu("Alternative")
		d := a.altDlg
		if d == nil || d.ErrorText() == "" || d.Loading() || !d.RetryVisible() {
			t.Fatalf("the window should show the failure at once")
		}
		if !strings.Contains(logs.String(), "alternatives failed") {
			t.Errorf("logs:\n%s", logs.String())
		}
		a.closeAlternatives()
	})
}

func TestClosingTheWindowCancelsTheRequest(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["late"]}`, Delay: 5 * time.Second, Info: info})
	h := newHarness(t, fake)
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitFor("the request to start", func() bool { return len(fake.Calls()) == 1 })

	var dlg *ui.AlternativesDialog
	onUI(func() {
		dlg = h.a.altDlg
		dlg.Close()
		if h.a.altDlg != nil {
			t.Error("closing the window should forget it")
		}
	})
	h.settle() // the request must end by itself, long before its 5 s delay
	onUI(func() {
		if len(dlg.Alternatives()) != 0 {
			t.Error("a closed window must not receive an answer")
		}
		if h.a.win.ErrorText() != "" {
			t.Errorf("cancelling is not an error: %q", h.a.win.ErrorText())
		}
	})
	if strings.Contains(h.logs.String(), "level=ERROR") || strings.Contains(h.logs.String(), "level=WARN") {
		t.Errorf("cancelling must not log above info:\n%s", h.logs.String())
	}
}

func TestAskingAgainReplacesTheWindow(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: `{"alternatives": ["slow"]}`, Delay: 5 * time.Second},
		altReply("fast"),
	)
	h := newHarness(t, fake)
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitFor("the first request to start", func() bool { return len(fake.Calls()) == 1 })
	first := h.altDialog()

	h.askAlternatives(12, 20) // "She have"
	h.waitAlternatives()
	onUI(func() {
		second := h.a.altDlg
		if second == first || first.IsVisible() {
			t.Error("the first window should be closed and replaced")
		}
		if got := second.Alternatives(); !slices.Equal(got, []string{"fast"}) || second.SelectionText() != "She have" {
			t.Errorf("second window: %q for %q", got, second.SelectionText())
		}
		if len(first.Alternatives()) != 0 {
			t.Error("the answer of the first request must not reach the first window")
		}
	})
}

func TestAskingWithTheWindowOpenAsksAgain(t *testing.T) {
	fake := llmtest.New(altReply("huge"))
	h := newHarness(t, fake)
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitAlternatives()
	// A second selection while the first answer is shown asks again.
	h.askAlternatives(0, 2)
	h.waitFor("the second request", func() bool { return len(fake.Calls()) == 2 })
	h.waitAlternatives()
	onUI(func() {
		if got := h.a.altDlg.SelectionText(); got != "He" {
			t.Errorf("selection = %q", got)
		}
	})
}

func TestDoubleClickReplacesTheSelectionAndClosesTheWindow(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		start, end int // UTF-16 units
		row        int
		want       string
	}{
		{"a word", "He go home. She have a cat.", 3, 5, 1, "He goes home. She have a cat."},
		{"a sentence", "He go home. She have a cat.", 12, 27, 0, "He go home. She has a cat."},
		{"after an emoji", "😀😀 He go home.", 8, 10, 1, "😀😀 He goes home."},
		{"a paragraph into two", "He go home.\n\nShe has a cat.", 0, 11, 2, "He goes home.\nHe is home.\n\nShe has a cat."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alts := map[string][]string{
				"a word":               {"went", "goes"},
				"a sentence":           {"She has a cat.", "She owns a cat."},
				"after an emoji":       {"went", "goes"},
				"a paragraph into two": {"x", "y", "He goes home.\nHe is home."},
			}[tt.name]
			h := newHarness(t, llmtest.New(altReply(alts...)))
			h.setText(tt.text)
			onUI(func() { h.a.win.SetModified(false) })
			h.askAlternatives(tt.start, tt.end)
			h.waitAlternatives()
			var dlg *ui.AlternativesDialog
			onUI(func() {
				dlg = h.a.altDlg
				dlg.DoubleClickRow(tt.row)
				if got := h.a.win.Text(); got != tt.want {
					t.Errorf("text = %q, want %q", got, tt.want)
				}
				if h.a.altDlg != nil || dlg.IsVisible() {
					t.Error("the window should be closed")
				}
				if !h.a.win.Modified() {
					t.Error("the text should be marked as changed")
				}
				if h.a.win.ErrorText() != "" {
					t.Errorf("banner: %q", h.a.win.ErrorText())
				}
				h.a.win.Undo()
				if got := h.a.win.Text(); got != tt.text {
					t.Errorf("one undo should bring back %q, got %q", tt.text, got)
				}
			})
		})
	}
}

func TestReplacingDropsTheIssueUnderTheSelection(t *testing.T) {
	fake := llmtest.New(twoIssues(), altReply("He goes"))
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	if n := len(h.underlines()); n != 2 {
		t.Fatalf("underlines = %d", n)
	}
	h.askAlternatives(0, 5) // "He go", the first issue
	h.waitAlternatives()
	onUI(func() { h.a.altDlg.DoubleClickRow(0) })
	us := h.underlines()
	if len(us) != 1 || us[0].Start != 14 || us[0].End != 22 {
		t.Errorf("the replaced issue goes and the other one moves: %+v", us)
	}
}

func TestDoubleClickAfterTheTextChangedReplacesNothing(t *testing.T) {
	h := newHarness(t, llmtest.New(altReply("went", "goes")))
	h.setText(sample)
	h.askAlternatives(3, 5) // "go"
	h.waitAlternatives()
	onUI(func() {
		h.a.win.InsertTextAt(0, "Well, ") // the user kept typing: "go" moved
		before := h.a.win.Text()
		h.a.altDlg.DoubleClickRow(0)
		if h.a.win.Text() != before {
			t.Errorf("text changed to %q", h.a.win.Text())
		}
		d := h.a.altDlg
		if d == nil || !d.IsVisible() || !strings.Contains(d.ErrorText(), "text has changed") {
			t.Fatal("the window should stay open and say why")
		}
		if len(d.Alternatives()) != 2 || !d.CopyEnabled() || d.RetryVisible() {
			t.Error("the alternatives must stay available to copy")
		}
	})
}

func TestDoubleClickWhenTheSelectedWordsWereEditedInPlace(t *testing.T) {
	h := newHarness(t, llmtest.New(altReply("went", "goes")))
	h.setText(sample)
	h.askAlternatives(3, 5)
	h.waitAlternatives()
	onUI(func() {
		h.a.win.ReplaceRange(3, 5, "ran") // same place, other words
		h.a.altDlg.DoubleClickRow(0)
		if got := h.a.win.Text(); got != "He ran home. She have a cat." {
			t.Errorf("text = %q", got)
		}
		if h.a.altDlg == nil {
			t.Error("the window should stay open")
		}
	})
}

func TestDoubleClickWhenTheTextGotShorter(t *testing.T) {
	h := newHarness(t, llmtest.New(altReply("went")))
	h.setText(sample)
	h.askAlternatives(12, 20)
	h.waitAlternatives()
	onUI(func() {
		h.a.win.SetText("short")
		h.a.altDlg.DoubleClickRow(0)
		if h.a.win.Text() != "short" || h.a.altDlg == nil || h.a.altDlg.ErrorText() == "" {
			t.Errorf("text %q, window open %v", h.a.win.Text(), h.a.altDlg != nil)
		}
	})
}
