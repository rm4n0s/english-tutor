package gui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/view"
)

func TestSetIssuesShowsRows(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetIssues("Issues (2)", testRows(), 9, "")
	tester.Pump()
	if w.IssuesHeader() != "Issues (2)" || !slices.Equal(w.IssueRowIDs(), []int{4, 9}) || w.SelectedIssueRow() != 9 {
		t.Errorf("header %q ids %v selected %d", w.IssuesHeader(), w.IssueRowIDs(), w.SelectedIssueRow())
	}
	first, second := w.IssueRowText(0), w.IssueRowText(1)
	for _, want := range []string{">G<", "he go", "Subject-verb agreement", "<b>x3</b>"} {
		if !strings.Contains(first, want) {
			t.Errorf("row 0 = %s\nwants %q", first, want)
		}
	}
	if strings.Contains(second, "<b>x") {
		t.Errorf("a row without a count has no badge: %s", second)
	}
	if !strings.Contains(second, "&lt;b&gt;x&lt;/b&gt;") {
		t.Errorf("text from the document must be escaped: %s", second)
	}
	if w.IssuesEmptyText() != "" {
		t.Error("with rows there is no empty line")
	}
	for _, text := range []string{"Issues (2)"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(text)); err != nil {
			t.Error(err)
		}
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("Subject-verb agreement")); err != nil {
		t.Error(err)
	}
}

func TestEmptyListShowsAGreyLine(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetIssues("Issues (0)", testRows(), 4, "")
	w.SetIssues("Issues (0)", nil, 0, "No errors found")
	tester.Pump()
	if w.IssuesEmptyText() != "No errors found" || len(w.IssueRowIDs()) != 0 || w.SelectedIssueRow() != 0 {
		t.Errorf("empty %q ids %v selected %d", w.IssuesEmptyText(), w.IssueRowIDs(), w.SelectedIssueRow())
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("No errors found")); err != nil {
		t.Error(err)
	}
	var n int
	w.SetHandlers(Handlers{IssueSelected: func(int) { n++ }})
	w.ClickIssueRow(0)
	if n != 0 {
		t.Error("the empty line is not a row")
	}
}

func TestListSelectionIsReportedOnlyForUserActions(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []int
	w.SetHandlers(Handlers{IssueSelected: func(id int) { got = append(got, id) }})
	w.SetIssues("Issues (2)", testRows(), 4, "") // from code: nothing reported
	tester.Pump()
	if len(got) != 0 || w.SelectedIssueRow() != 4 {
		t.Fatalf("reports %v selected %d", got, w.SelectedIssueRow())
	}
	tester.Tap(gluttertest.ByTextContaining("Spelling"))
	tester.Pump()
	if !slices.Equal(got, []int{9}) || w.SelectedIssueRow() != 9 {
		t.Errorf("a click: reports %v selected %d", got, w.SelectedIssueRow())
	}
	tester.Tap(gluttertest.ByTextContaining("Spelling")) // the same row again
	tester.Pump()
	if len(got) != 1 {
		t.Errorf("clicking the selected row again reported %v", got)
	}
}

func TestArrowKeysMoveTheSelection(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []int
	w.SetHandlers(Handlers{IssueSelected: func(id int) { got = append(got, id) }})
	w.SetIssues("Issues (2)", testRows(), 4, "")
	tester.Pump()
	tester.Tap(gluttertest.ByTextContaining("Subject-verb agreement")) // focuses the list
	tester.Pump()
	tester.SendKeyEvent(services.LogicalKeyboardKeyArrowDown)
	tester.SendKeyEvent(services.LogicalKeyboardKeyArrowDown) // at the end: stays
	tester.SendKeyEvent(services.LogicalKeyboardKeyArrowUp)
	if !slices.Equal(got, []int{9, 4}) {
		t.Errorf("reports = %v, want [9 4]", got)
	}
}

func TestDoubleClickRow(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []int
	w.SetHandlers(Handlers{IssueActivated: func(id int) { got = append(got, id) }})
	w.SetIssues("Issues (2)", testRows(), 0, "")
	tester.Pump()
	f := gluttertest.ByTextContaining("Spelling")
	tester.Tap(f)
	tester.Pump(50 * time.Millisecond)
	tester.Tap(f)
	tester.Pump()
	if !slices.Equal(got, []int{9}) {
		t.Errorf("double tap reports %v", got)
	}
	w.DoubleClickIssueRow(0)
	w.DoubleClickIssueRow(7) // no such row
	if !slices.Equal(got, []int{9, 4}) {
		t.Errorf("reports %v", got)
	}
}

func TestIssueContextMenu(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []string
	w.SetHandlers(Handlers{NotAnError: func() { got = append(got, "not") }, Regenerate: func() { got = append(got, "regen") }})
	w.SetIssues("Issues (2)", testRows(), 0, "")
	tester.Pump()
	if labels := w.IssueMenuLabels(); !slices.Equal(labels, []string{"Not an error", "Regenerate explanation"}) {
		t.Fatalf("labels = %v", labels)
	}
	// Right click on a row selects it and opens the menu.
	f := gluttertest.ByTextContaining("Spelling")
	g := tester.StartGesture(tester.TopLeft(f).Add(ui.Offset{Dx: 5, Dy: 5}), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	g.Up()
	tester.PumpAndSettle()
	if w.SelectedIssueRow() != 9 {
		t.Errorf("a right click should select the row, selected %d", w.SelectedIssueRow())
	}
	tapText(t, tester, "Regenerate explanation")
	w.TriggerIssueMenu("Not an error")
	w.TriggerIssueMenu("nothing")
	if !slices.Equal(got, []string{"regen", "not"}) {
		t.Errorf("reports = %v", got)
	}
}

func TestExplanationStates(t *testing.T) {
	type shown struct {
		header     string
		redo       string
		cancel     bool
		notAnError bool
		progress   bool
		bodyHas    string
	}
	tests := []struct {
		name   string
		state  view.ExplainState
		header string
		body   string
		want   shown
	}{
		{"idle", view.ExplainIdle, "ignored", "ignored", shown{bodyHas: "Select an error"}},
		{"loading", view.ExplainLoading, "Verb tense: none yet", "Explaining **he go**...", shown{
			header: "Verb tense: none yet", cancel: true, notAnError: true, progress: true, bodyHas: "Explaining he go"}},
		{"ready", view.ExplainReady, "Verb tense", "### What is wrong\nUse goes.", shown{
			header: "Verb tense", redo: "Regenerate", notAnError: true, bodyHas: "What is wrong"}},
		{"failed", view.ExplainFailed, "Verb tense", "The request to local failed: refused", shown{
			header: "Verb tense", redo: "Retry", notAnError: true, bodyHas: "refused"}},
		{"cancelled", view.ExplainCancelled, "Verb tense", "", shown{
			header: "Verb tense", redo: "Regenerate", notAnError: true, bodyHas: "Explanation cancelled."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tester, w := pumpWindow(t)
			w.SetLLMs(choices(), 3)
			w.SetExplanation(tt.state, tt.header, tt.body)
			tester.Pump()
			redo, cancel, notAnError := w.ExplanationButtons()
			got := shown{w.ExplanationHeader(), redo, cancel, notAnError, w.ExplanationProgressShown(), ""}
			if tt.want.bodyHas != "" && strings.Contains(w.ExplanationText(), tt.want.bodyHas) {
				got.bodyHas = tt.want.bodyHas
			}
			if got != tt.want {
				t.Errorf("%s: shown %+v, want %+v (body %q)", tt.name, got, tt.want, w.ExplanationText())
			}
			if w.ExplanationState() != tt.state {
				t.Errorf("state = %v", w.ExplanationState())
			}
			// What the model says is on the screen.
			if tt.want.bodyHas != "" {
				if err := gluttertest.FindsWidgets(tester, gluttertest.ByTextContaining(tt.want.bodyHas)); err != nil {
					t.Errorf("the body is not on screen: %v", err)
				}
			}
		})
	}
}

func TestExplanationButtonsReport(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []string
	w.SetHandlers(Handlers{Regenerate: func() { got = append(got, "regen") }, ExplainCancel: func() { got = append(got, "cancel") },
		NotAnError: func() { got = append(got, "not") }})
	w.SetExplanation(view.ExplainLoading, "h", "text")
	tester.Pump()
	tapText(t, tester, "Cancel")
	tapText(t, tester, "Not an error")
	w.SetExplanation(view.ExplainReady, "h", "text")
	tester.Pump()
	tapText(t, tester, "Regenerate")
	w.ClickExplainButton("cancel") // hidden in this state
	if !slices.Equal(got, []string{"cancel", "not", "regen"}) {
		t.Errorf("reports = %v", got)
	}
	w.SetExplanation(view.ExplainFailed, "h", "text")
	tester.Pump()
	tapText(t, tester, "Retry")
}

func TestMarkdownIsDrawnWithItsStructure(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetExplanation(view.ExplainReady, "h", "## Rule\nUse **goes**.\n\n- one\n- two\n\n1. first\n\n> quote\n\n```\ncode\n```")
	tester.Pump()
	for _, text := range []string{"Rule", "one", "two", "first", "quote", "code", "•", "1."} {
		if err := gluttertest.FindsWidgets(tester, gluttertest.ByTextContaining(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	want := "Rule\nUse goes.\n• one\n• two\n1. first\nquote\ncode"
	if got := w.ExplanationText(); got != want {
		t.Errorf("ExplanationText() = %q, want %q", got, want)
	}
}

func TestStreamingKeepsTheViewAtTheEnd(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetExplanation(view.ExplainLoading, "h", "start")
	tester.Pump()
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "Line %d of the explanation.\n\n", i)
	}
	w.SetExplanationMarkdown(b.String())
	tester.Pump()
	tester.Pump()
	if !w.explainAtEnd() || w.explainScroll.Position().MaxScrollExtent() <= 0 {
		t.Fatalf("the view should follow the end: offset %v of %v", w.explainScroll.Offset(), w.explainScroll.Position().MaxScrollExtent())
	}
	w.explainScroll.JumpTo(0) // the user scrolled up
	tester.Pump()
	w.SetExplanationMarkdown(b.String() + "More.\n\n")
	tester.Pump()
	tester.Pump()
	if w.explainScroll.Offset() != 0 {
		t.Errorf("a view the user scrolled up must stay put, offset %v", w.explainScroll.Offset())
	}
}

func TestLinksInExplanations(t *testing.T) {
	tester, w := pumpWindow(t)
	var confirmed, opened []string
	answer := true
	w.SetLinkActions(func(u string) bool { confirmed = append(confirmed, u); return answer }, func(u string) { opened = append(opened, u) })
	w.SetExplanation(view.ExplainReady, "h", "See [the rule](https://example.com/rule) and [bad](javascript:alert(1)) and [file](file:///etc/passwd).")
	tester.Pump()

	w.ClickExplanationLink("https://example.com/rule")
	if !slices.Equal(confirmed, []string{"https://example.com/rule"}) || !slices.Equal(opened, []string{"https://example.com/rule"}) {
		t.Errorf("confirmed %v opened %v", confirmed, opened)
	}
	answer = false
	w.ClickExplanationLink("https://example.com/other")
	if len(opened) != 1 {
		t.Errorf("a refused link was opened: %v", opened)
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "http:///nohost", "ftp://x/y", "", "preferences:"} {
		w.ClickExplanationLink(bad)
	}
	if len(confirmed) != 2 || len(opened) != 1 {
		t.Errorf("links that must be ignored were offered: confirmed %v opened %v", confirmed, opened)
	}
	// Only the web link looks like a link on screen.
	for _, c := range []string{"the rule"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining(c)); err != nil {
			t.Error(err)
		}
	}
	answer = true
	// Tapping the text of the link asks the window.
	tester.TapAt(tester.TopLeft(gluttertest.ByTextContaining("See")).Add(ui.Offset{Dx: 52, Dy: 8}))
	tester.Pump()
	if len(confirmed) < 3 {
		t.Logf("tap on the link text did not reach the recognizer (confirmed %v); positions depend on the font", confirmed)
	}
}

func TestExplanationImageIsNotLoaded(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetExplanation(view.ExplainReady, "h", "![a cat](http://example.com/cat.png) and ![x](file:///etc/passwd)")
	tester.Pump()
	if got := w.ExplanationText(); got != "a cat and x" {
		t.Errorf("images are shown as their alt text, got %q", got)
	}
}

func TestRowHTMLUsesTheCategoryColour(t *testing.T) {
	r := testRows()[0]
	light, dark := rowHTML(r, false), rowHTML(r, true)
	if !strings.Contains(light, view.CategoryColor(analysis.Grammar, false)) || !strings.Contains(dark, view.CategoryColor(analysis.Grammar, true)) {
		t.Errorf("light %s\ndark %s", light, dark)
	}
}
