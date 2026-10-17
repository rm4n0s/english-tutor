package app

import (
	"io"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

type notice struct{ title, text string }

// notices makes the harness record the desktop messages and say whether the
// user is in the window. It returns the messages and the number of times the
// window asked for attention.
func (h *harness) notices(active bool) *[]notice {
	got := new([]notice)
	onUI(func() {
		h.a.windowActive = func() bool { return active }
		h.a.notifier = func(title, text string) { *got = append(*got, notice{title, text}) }
	})
	return got
}

func TestCheckEndNotifiesWhenTheUserIsElsewhere(t *testing.T) {
	tests := []struct {
		name   string
		active bool
		auto   bool
		reply  llmtest.Reply
		want   []notice
	}{
		{"result while in another application", false, false, twoIssues(), []notice{{"Check finished", "2 issues"}}},
		{"no mistakes", false, false, llmtest.Reply{Text: issuesReply(nil), Info: info}, []notice{{"Check finished", "No errors found"}}},
		{"failure says so", false, false, llmtest.Reply{Err: io.ErrUnexpectedEOF}, []notice{{"Check failed", "The request to local failed: unexpected EOF"}}},
		{"the user is in the window and sees it", true, false, twoIssues(), nil},
		{"auto-check is silent", false, true, twoIssues(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, llmtest.New(tt.reply))
			got := h.notices(tt.active)
			h.setText(sample)
			if tt.auto {
				onUI(func() { h.a.startCheck(false) })
			} else {
				h.checkButton()
			}
			h.waitIdle()
			if len(*got) != len(tt.want) {
				t.Fatalf("notices = %+v, want %+v", *got, tt.want)
			}
			for i := range tt.want {
				if (*got)[i] != tt.want[i] {
					t.Errorf("notice %d = %+v, want %+v", i, (*got)[i], tt.want[i])
				}
			}
		})
	}
}

func TestCancelledCheckDoesNotNotify(t *testing.T) {
	h := newHarness(t, llmtest.New(llmtest.Reply{Text: issuesReply(nil), Delay: 5 * time.Second}))
	got := h.notices(false)
	h.setText(sample)
	h.checkButton()
	onUI(func() { h.a.analyzer.Cancel() })
	h.waitIdle()
	if len(*got) != 0 {
		t.Errorf("a check the user cancelled must not notify, got %+v", *got)
	}
}
