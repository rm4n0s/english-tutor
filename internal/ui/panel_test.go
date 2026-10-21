package ui

import (
	"github.com/rm4n0s/english-tutor/internal/view"
	"slices"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
)

func row(id int, typeID, snippet string, count int) issues.Row {
	return issues.RowFor(issues.Item{
		ID:    id,
		Issue: analysis.Issue{Text: snippet, Type: analysis.Resolve(typeID)},
	}, count)
}

func threeRows() []issues.Row {
	return []issues.Row{
		row(1, "subject_verb_agreement", "He go to school", 7),
		row(2, "collocation", "make a photo", 0),
		row(3, "word_order", "I very like it", 2),
	}
}

func TestSetIssuesShowsRows(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetIssues("Issues (3)", threeRows(), 0, "")
		if w.IssuesHeader() != "Issues (3)" || !slices.Equal(w.IssueRowIDs(), []int{1, 2, 3}) {
			t.Errorf("header %q, rows %v", w.IssuesHeader(), w.IssueRowIDs())
		}
		if w.IssuesEmptyText() != "" {
			t.Error("with rows there is no empty line")
		}
		first, second := w.IssueRowText(0), w.IssueRowText(1)
		for _, want := range []string{">G<", "He go to school", "Subject-verb agreement", "<b>x7</b>", view.CategoryColor(analysis.Grammar, w.isDark())} {
			if !strings.Contains(first, want) {
				t.Errorf("first row missing %q:\n%s", want, first)
			}
		}
		if strings.Contains(second, "<b>x") || !strings.Contains(second, ">V<") || !strings.Contains(second, "Collocation") {
			t.Errorf("second row: a count of 0 hides the badge, category V:\n%s", second)
		}
		if w.SelectedIssueRow() != 0 {
			t.Error("nothing should be selected")
		}
	})
}

func TestSetIssuesEscapesHTML(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetIssues("Issues (1)", []issues.Row{row(1, "spelling", `<b onmouseover="x">&lt;script&gt;`, 0)}, 0, "")
		text := w.IssueRowText(0)
		if strings.Contains(text, "<b onmouseover") || !strings.Contains(text, "&lt;b onmouseover") || !strings.Contains(text, "&amp;lt;script") {
			t.Errorf("the snippet comes from the user's text and must be escaped:\n%s", text)
		}
	})
}

func TestEmptyListShowsAGreyLine(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetIssues("Issues (0)", nil, 0, "No errors found")
		if w.IssuesEmptyText() != "No errors found" || len(w.IssueRowIDs()) != 0 {
			t.Errorf("empty text %q, rows %v", w.IssuesEmptyText(), w.IssueRowIDs())
		}
		if w.issuesList.Item(0).Flags()&0x20 != 0 && w.issuesList.CurrentRow() >= 0 && w.SelectedIssueRow() != 0 {
			t.Error("the grey line is not selectable")
		}
		clicked := 0
		w.SetHandlers(Handlers{IssueSelected: func(int) { clicked++ }})
		w.ClickIssueRow(0)
		if clicked != 0 {
			t.Error("selecting the grey line must not report an issue")
		}
	})
}

func TestListSelectionIsReportedOnlyForUserActions(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var got []int
		w.SetHandlers(Handlers{IssueSelected: func(id int) { got = append(got, id) }})
		w.SetIssues("Issues (3)", threeRows(), 2, "") // from code: no report
		if w.SelectedIssueRow() != 2 || len(got) != 0 {
			t.Fatalf("selected %d, reports %v", w.SelectedIssueRow(), got)
		}
		w.ClickIssueRow(2) // the user
		w.ClickIssueRow(0)
		if !slices.Equal(got, []int{3, 1}) {
			t.Errorf("reports = %v, want [3 1]", got)
		}
		w.SetIssues("Issues (3)", threeRows(), 0, "") // selection cleared from code
		if w.SelectedIssueRow() != 0 || len(got) != 2 {
			t.Errorf("selected %d, reports %v", w.SelectedIssueRow(), got)
		}
	})
}

func TestDoubleClickRow(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var got []int
		w.SetHandlers(Handlers{IssueActivated: func(id int) { got = append(got, id) }})
		w.SetIssues("Issues (3)", threeRows(), 0, "")
		w.DoubleClickIssueRow(1)
		w.DoubleClickIssueRow(7) // no such row
		if !slices.Equal(got, []int{2}) {
			t.Errorf("activated = %v", got)
		}
	})
}

func TestIssueContextMenu(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var calls []string
		w.SetHandlers(Handlers{
			NotAnError: func() { calls = append(calls, "notanerror") },
			Regenerate: func() { calls = append(calls, "regenerate") },
		})
		if got := w.IssueMenuLabels(); !slices.Equal(got, []string{"Not an error", "Regenerate explanation"}) {
			t.Errorf("menu = %v", got)
		}
		w.TriggerIssueMenu("Regenerate explanation")
		w.TriggerIssueMenu("Not an error")
		if !slices.Equal(calls, []string{"regenerate", "notanerror"}) {
			t.Errorf("calls = %v", calls)
		}
	})
}

func TestExplanationStates(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetLLMs([]LLMChoice{{ID: 1, Name: "local"}}, 1)
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
			{"loading", view.ExplainLoading, "Verb tense: no mistake of this type recorded yet", "Explaining **he go**...", shown{
				header: "Verb tense: no mistake of this type recorded yet", cancel: true, notAnError: true, progress: true, bodyHas: "Explaining he go"}},
			{"ready", view.ExplainReady, "Verb tense", "### What is wrong\nUse goes.", shown{
				header: "Verb tense", redo: "Regenerate", notAnError: true, bodyHas: "What is wrong"}},
			{"failed", view.ExplainFailed, "Verb tense", "The request to local failed: refused", shown{
				header: "Verb tense", redo: "Retry", notAnError: true, bodyHas: "refused"}},
			{"cancelled", view.ExplainCancelled, "Verb tense", "", shown{
				header: "Verb tense", redo: "Regenerate", notAnError: true, bodyHas: "Explanation cancelled."}},
		}
		for _, tt := range tests {
			w.SetExplanation(tt.state, tt.header, tt.body)
			redo, cancel, notAnError := w.ExplanationButtons()
			got := shown{w.ExplanationHeader(), redo, cancel, notAnError, w.ExplanationProgressShown(), ""}
			if !strings.Contains(w.ExplanationText(), tt.want.bodyHas) {
				t.Errorf("%s: body = %q, want it to contain %q", tt.name, w.ExplanationText(), tt.want.bodyHas)
			}
			tt.want.bodyHas = ""
			if got != tt.want {
				t.Errorf("%s: view = %+v, want %+v", tt.name, got, tt.want)
			}
			if w.ExplanationState() != tt.state {
				t.Errorf("%s: state = %v", tt.name, w.ExplanationState())
			}
		}
	})
}

func TestExplanationButtonsReport(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var calls []string
		w.SetHandlers(Handlers{
			Regenerate:    func() { calls = append(calls, "regenerate") },
			ExplainCancel: func() { calls = append(calls, "cancel") },
			NotAnError:    func() { calls = append(calls, "notanerror") },
		})
		w.ClickExplainButton("redo")
		w.ClickExplainButton("cancel")
		w.ClickExplainButton("notanerror")
		w.ClickExplainButton("nonsense")
		if !slices.Equal(calls, []string{"regenerate", "cancel", "notanerror"}) {
			t.Errorf("calls = %v", calls)
		}
	})
}

func TestStreamingKeepsTheViewAtTheEnd(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.win.Resize(900, 600)
		w.Show()
		w.SetExplanation(view.ExplainLoading, "h", "start")
		long := strings.Repeat("A line of the explanation.\n\n", 200)
		w.SetExplanationMarkdown(long)
		bar := w.explain.VerticalScrollBar()
		if bar.Maximum() == 0 {
			t.Skip("the text did not overflow the panel")
		}
		if bar.Value() != bar.Maximum() {
			t.Errorf("the view should follow the end: %d of %d", bar.Value(), bar.Maximum())
		}
		bar.SetValue(0)
		w.SetExplanationMarkdown(long + "more\n\n")
		if bar.Value() != 0 {
			t.Errorf("a view the user scrolled up must stay put, got %d", bar.Value())
		}
	})
}

func TestLinksInExplanations(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var asked, opened []string
		prefs := 0
		answer := true
		w.SetLinkActions(
			func(url string) bool { asked = append(asked, url); return answer },
			func(url string) { opened = append(opened, url) },
		)
		w.SetHandlers(Handlers{Preferences: func() { prefs++ }})

		w.SetExplanation(view.ExplainReady, "h", "[rule](https://example.com/rule)")
		w.ClickExplanationLink("https://example.com/rule")
		if !slices.Equal(asked, []string{"https://example.com/rule"}) || !slices.Equal(opened, []string{"https://example.com/rule"}) {
			t.Errorf("a confirmed https link: asked %v, opened %v", asked, opened)
		}

		answer = false
		w.ClickExplanationLink("http://example.com/other")
		if len(asked) != 2 || len(opened) != 1 {
			t.Errorf("a declined link must not open: asked %v, opened %v", asked, opened)
		}

		for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com", "mailto:a@b.c", "preferences:"} {
			w.ClickExplanationLink(bad)
		}
		if len(asked) != 2 || len(opened) != 1 || prefs != 0 {
			t.Errorf("other schemes are ignored, and preferences: only works in the hint: asked %v opened %v prefs %d", asked, opened, prefs)
		}

		w.SetExplanation(view.ExplainIdle, "", "")
		w.ClickExplanationLink("preferences:")
		if prefs != 1 {
			t.Errorf("the hint's preferences link must open Preferences, got %d", prefs)
		}
	})
}

func TestNoResourcesAreLoaded(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		// An image in an explanation must not be fetched or read from disk: the
		// browser's resource loader returns nothing for every URL.
		w.SetExplanation(view.ExplainReady, "h", "![x](file:///etc/hostname) and ![y](https://example.com/a.png) <img src=\"file:///etc/hostname\">")
		for _, u := range []string{"file:///etc/hostname", "https://example.com/a.png"} {
			if v := w.explain.LoadResource(2, qt.NewQUrl3(u)); v.IsValid() {
				t.Errorf("resource %s was loaded", u)
			}
		}
	})
}
