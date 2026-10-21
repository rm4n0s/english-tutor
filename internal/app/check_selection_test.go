package app

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

// checkSelection selects the UTF-16 range and chooses Check from the editor's
// right-click menu, as the user would.
func (h *harness) checkSelection(start, end int) {
	h.t.Helper()
	h.onUI(func() {
		h.a.win.SelectRange(start, end)
		if !h.a.win.TriggerEditorMenu("Check") {
			h.t.Error("the menu has no Check entry")
		}
	})
}

func (h *harness) editorBusy() (busy bool) {
	h.onUI(func() { busy = h.a.win.EditorBusy() })
	return busy
}

func oneIssue(text, typ string) llmtest.Reply {
	return llmtest.Reply{Text: issuesReply(map[string]string{text: typ}), Info: info}
}

func TestCheckSelectionSendsOnlyTheSelection(t *testing.T) {
	fake := llmtest.New(oneIssue("She have", "spelling"))
	h := newHarness(t, fake)
	h.setText(sample) // "He go home. She have a cat."
	h.checkSelection(12, 27)
	h.waitIdle()

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if !strings.Contains(calls[0].Prompt, "She have a cat.") || strings.Contains(calls[0].Prompt, "He go home") {
		t.Errorf("only the selection may be sent, prompt:\n%s", calls[0].Prompt)
	}
	us := h.underlines()
	if len(us) != 1 || us[0].Start != 12 || us[0].End != 20 || us[0].Category != analysis.Vocabulary {
		t.Fatalf("underlines = %+v, want one at 12..20 of the whole text", us)
	}
	h.onUI(func() {
		if got := h.a.win.StatusText(); got != "1 issue" {
			t.Errorf("status = %q", got)
		}
		if got := h.a.win.TokensText(); got != "last: 120 in / 30 out · session: 120 in / 30 out" {
			t.Errorf("tokens = %q", got)
		}
		if h.a.win.Text() != sample || h.a.win.Modified() {
			t.Error("checking must not change the text")
		}
	})
	h.waitFor("the mistake to be recorded", func() bool { return h.events() == 1 })
}

func TestCheckSelectionUsesUTF16Positions(t *testing.T) {
	text := "😀 He go home. She have a cat."
	fake := llmtest.New(oneIssue("He go", "verb_form"))
	h := newHarness(t, fake)
	h.setText(text)
	h.checkSelection(3, 14) // "He go home."
	h.waitIdle()

	us := h.underlines()
	if len(us) != 1 || us[0].Start != 3 || us[0].End != 8 {
		t.Fatalf("underlines = %+v, want 3..8 (the emoji is two units)", us)
	}
	if !strings.Contains(fake.Calls()[0].Prompt, "He go home.") {
		t.Errorf("prompt:\n%s", fake.Calls()[0].Prompt)
	}
}

func TestCheckSelectionReplacesOnlyTheIssuesInTheSelection(t *testing.T) {
	tests := []struct {
		name       string
		reply      llmtest.Reply
		start, end int
		want       []gui.Underline
	}{
		{
			name:  "nothing found in the selection",
			reply: llmtest.Reply{Text: issuesReply(nil), Info: info},
			start: 0, end: 11,
			want: []gui.Underline{{Start: 12, End: 20, Category: analysis.Vocabulary}},
		},
		{
			name:  "another issue found in the selection",
			reply: oneIssue("home", "wrong_word"),
			start: 0, end: 11,
			want: []gui.Underline{{Start: 6, End: 10, Category: analysis.Vocabulary}, {Start: 12, End: 20, Category: analysis.Vocabulary}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, llmtest.New(twoIssues(), tt.reply))
			h.setText(sample)
			h.checkButton()
			h.waitIdle()
			if n := len(h.underlines()); n != 2 {
				t.Fatalf("after the full check: %d underlines", n)
			}

			h.checkSelection(tt.start, tt.end)
			h.waitIdle()
			got := h.underlines()
			if len(got) != len(tt.want) {
				t.Fatalf("underlines = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("underline %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
			h.onUI(func() {
				if want := len(tt.want); len(h.a.state.Items()) != want {
					t.Errorf("state has %d issues, want %d", len(h.a.state.Items()), want)
				}
			})
		})
	}
}

func TestCheckSelectionAfterAFullCheckDoesNotCountTwice(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues(), oneIssue("She have", "spelling")))
	h.setText(sample)
	h.checkButton()
	h.waitIdle()
	h.waitFor("recording", func() bool { return h.events() == 2 })

	h.checkSelection(12, 27) // the same mistake in the same text
	h.waitIdle()
	h.settle()
	h.sleep(50 * time.Millisecond)
	if n := h.events(); n != 2 {
		t.Errorf("events = %d, want 2: the mistake is counted once whatever part was checked", n)
	}
}

func TestCheckSelectionOfAnUncheckedTextRecordsWhatItFinds(t *testing.T) {
	h := newHarness(t, llmtest.New(oneIssue("go", "verb_form")))
	h.setText(sample)
	h.checkSelection(0, 11)
	h.waitIdle()
	h.waitFor("recording", func() bool { return h.events() == 1 })
	h.onUI(func() {
		if !h.a.resultRecorded {
			t.Error("a manual check of a text without other issues leaves a recorded result")
		}
	})
}

func TestCheckSelectionKeepsAnUnrecordedResultUnrecorded(t *testing.T) {
	h := newHarness(t, llmtest.New(twoIssues(), oneIssue("go", "verb_form")))
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) }) // an auto-check: not recorded
	h.waitIdle()
	h.checkSelection(0, 11)
	h.waitIdle()
	h.onUI(func() {
		if h.a.resultRecorded {
			t.Error("the issues of the auto-check are not in the statistics, so the result is not recorded")
		}
	})
}

func TestCheckSelectionIgnoresASelectionOfWhiteSpace(t *testing.T) {
	fake := llmtest.New(oneIssue("go", "verb_form"))
	h := newHarness(t, fake)
	h.setText(sample)
	h.onUI(func() { h.a.onCheckSelection(11, 12) }) // the space between the sentences
	h.onUI(func() { h.a.onCheckSelection(5, 5) })
	if h.a.checking || len(fake.Calls()) != 0 {
		t.Errorf("checking = %v, calls = %d: nothing should have started", h.a.checking, len(fake.Calls()))
	}
}

func TestCheckSelectionIsOfferedOnlyWithAnLLM(t *testing.T) {
	h := newHarness(t, llmtest.New())
	h.setText(sample)
	h.onUI(func() {
		h.a.win.SelectRange(0, 11)
		h.a.win.SetLLMs(nil, 0)
		if h.a.win.EditorMenuEnabled("Check") {
			t.Error("Check must be disabled without an LLM")
		}
	})
}

func TestTheEditorIsCoveredWhileACheckRuns(t *testing.T) {
	tests := []struct {
		name  string
		start func(h *harness)
	}{
		{"the Check button", func(h *harness) { h.checkButton() }},
		{"the menu entry", func(h *harness) { h.checkSelection(0, 11) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(llmtest.Reply{Text: issuesReply(nil), Info: info, Delay: 200 * time.Millisecond})
			h := newHarness(t, fake)
			h.setText(sample)
			if h.editorBusy() {
				t.Fatal("the editor is covered before any check")
			}
			tt.start(h)
			if !h.editorBusy() {
				t.Error("the editor should be covered while the check runs")
			}
			h.onUI(func() {
				before := h.a.win.Text()
				h.a.win.TypeIntoEditor("X")
				if h.a.win.Text() != before {
					t.Errorf("the text changed under the loading screen: %q", h.a.win.Text())
				}
			})
			h.waitIdle()
			if h.editorBusy() {
				t.Error("the cover should go when the check ends")
			}
			h.onUI(func() {
				h.a.win.TypeIntoEditor("X")
				if !strings.HasSuffix(h.a.win.Text(), "X") {
					t.Errorf("the editor should accept text again: %q", h.a.win.Text())
				}
			})
		})
	}
}

func TestTheEditorIsNotCoveredByAnAutoCheck(t *testing.T) {
	h := newHarness(t, llmtest.New(llmtest.Reply{Text: issuesReply(nil), Info: info, Delay: 100 * time.Millisecond}))
	h.setText(sample)
	h.onUI(func() { h.a.startCheck(false) })
	if h.editorBusy() {
		t.Error("an auto-check fires while the user types; it must not lock the editor")
	}
	h.waitIdle()
}

func TestTheCoverGoesWhenACheckIsCancelledOrFails(t *testing.T) {
	tests := []struct {
		name  string
		reply llmtest.Reply
		end   func(h *harness, fake *llmtest.Fake)
	}{
		{"cancelled", llmtest.Reply{Text: issuesReply(nil), Delay: 5 * time.Second}, func(h *harness, fake *llmtest.Fake) {
			// Cancel only reaches a request that has started.
			h.waitFor("the request", func() bool { return len(fake.Calls()) == 1 })
			h.checkButton() // the Cancel button
		}},
		// The failure comes after a delay, so that the test can see the cover before it goes.
		{"failed", llmtest.Reply{Err: io.ErrUnexpectedEOF, Delay: 100 * time.Millisecond}, func(*harness, *llmtest.Fake) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(tt.reply)
			h := newHarness(t, fake)
			h.setText(sample)
			h.checkSelection(0, 11)
			if !h.editorBusy() {
				t.Fatal("the editor should be covered")
			}
			tt.end(h, fake)
			h.waitIdle()
			if h.editorBusy() {
				t.Error("the cover must go when the check ends, whatever the outcome")
			}
		})
	}
}

func TestTheCoverGoesWhenANewTextIsLoaded(t *testing.T) {
	h := newHarness(t, llmtest.New(llmtest.Reply{Text: issuesReply(nil), Delay: 5 * time.Second}))
	h.setText(sample)
	h.checkButton()
	if !h.editorBusy() {
		t.Fatal("the editor should be covered")
	}
	h.onUI(func() { h.a.resetText("Another text.") })
	if h.editorBusy() {
		t.Error("a new text stops the check, so the cover must go")
	}
}

func TestEditsDuringASelectionCheckAreAppliedToTheResult(t *testing.T) {
	// Typing is blocked by the cover, but a program (such as the Alternatives
	// window) can still change the text; the result is then rebased.
	fake := llmtest.New(llmtest.Reply{Text: issuesReply(map[string]string{"She have": "spelling"}), Info: info, Delay: 100 * time.Millisecond})
	h := newHarness(t, fake)
	h.setText(sample)
	h.checkSelection(12, 27)
	h.waitFor("the request", func() bool { return len(fake.Calls()) == 1 })
	h.onUI(func() { h.a.win.InsertTextAt(0, "Oh. ") }) // before the selection: everything moves by 4
	h.waitIdle()
	us := h.underlines()
	if len(us) != 1 || us[0].Start != 16 || us[0].End != 24 {
		t.Fatalf("underlines = %+v, want the issue moved to 16..24", us)
	}
}
