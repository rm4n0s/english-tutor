package gui

import (
	"fmt"
	"html"

	"github.com/rm4n0s/glutter/gestures"

	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/view"
)

// ---- Issues list (section 4.3) ----

// issueMenuLabels are the entries of the right-click menu of a row.
var issueMenuLabels = []string{"Not an error", "Regenerate explanation"}

// SetIssues fills the issues list. With no rows the list shows emptyText as one
// grey line. selectedID is the row to select (0 for none); selecting from code
// reports nothing.
func (w *MainWindow) SetIssues(header string, rows []issues.Row, selectedID int, emptyText string) {
	w.issuesHeader = header
	w.rows = append([]issues.Row(nil), rows...)
	w.rowIDs = nil
	for _, r := range rows {
		w.rowIDs = append(w.rowIDs, r.ID)
	}
	w.selectedID = 0
	for _, id := range w.rowIDs {
		if id == selectedID {
			w.selectedID = id
		}
	}
	w.emptyText = emptyText
	w.refresh()
}

// rowHTML is the text of one list row: the category letter in the category
// colour, the snippet, and on a second line the error type and the count badge.
// The list draws the same from the row; this form is what tests read.
func rowHTML(r issues.Row, dark bool) string {
	badge := ""
	if b := r.Badge(); b != "" {
		badge = "&nbsp;&nbsp;<b>" + html.EscapeString(b) + "</b>"
	}
	return fmt.Sprintf(`<span style="color:%s; font-weight:bold">%s</span>&nbsp;%s<br/><span style="color:gray">%s%s</span>`,
		view.CategoryColor(r.Category, dark), html.EscapeString(r.Marker), html.EscapeString(r.Snippet),
		html.EscapeString(r.Label), badge)
}

// IssuesHeader returns the title above the list.
func (w *MainWindow) IssuesHeader() string { return w.issuesHeader }

// IssueRowIDs returns the issue ID of each row (empty if the list shows its
// empty line).
func (w *MainWindow) IssueRowIDs() []int { return append([]int(nil), w.rowIDs...) }

// IssueRowText returns the rich text of a row.
func (w *MainWindow) IssueRowText(row int) string {
	if row < 0 || row >= len(w.rows) {
		return ""
	}
	return rowHTML(w.rows[row], w.prefersDark)
}

// IssuesEmptyText returns the grey line shown instead of rows, or "".
func (w *MainWindow) IssuesEmptyText() string {
	if len(w.rows) == 0 {
		return w.emptyText
	}
	return ""
}

// SelectedIssueRow returns the ID of the selected row, or 0.
func (w *MainWindow) SelectedIssueRow() int { return w.selectedID }

// ClickIssueRow selects a row as the user would with the mouse or the keys.
func (w *MainWindow) ClickIssueRow(row int) {
	if row < 0 || row >= len(w.rowIDs) {
		return
	}
	id := w.rowIDs[row]
	changed := id != w.selectedID
	w.selectedID = id
	w.refresh()
	if changed && w.handlers.IssueSelected != nil {
		w.handlers.IssueSelected(id)
	}
}

// stepIssueRow moves the selection of the list by delta rows, as the arrow keys
// do.
func (w *MainWindow) stepIssueRow(delta int) {
	if len(w.rowIDs) == 0 {
		return
	}
	cur := -1
	for i, id := range w.rowIDs {
		if id == w.selectedID {
			cur = i
		}
	}
	w.ClickIssueRow(min(max(cur+delta, 0), len(w.rowIDs)-1))
}

// DoubleClickIssueRow double-clicks a row.
func (w *MainWindow) DoubleClickIssueRow(row int) {
	if row >= 0 && row < len(w.rowIDs) && w.handlers.IssueActivated != nil {
		w.handlers.IssueActivated(w.rowIDs[row])
	}
}

// IssueMenuLabels returns the labels of the row context menu.
func (w *MainWindow) IssueMenuLabels() []string { return append([]string(nil), issueMenuLabels...) }

// TriggerIssueMenu triggers one entry of the row context menu by its label.
func (w *MainWindow) TriggerIssueMenu(label string) {
	switch label {
	case "Not an error":
		call(w.handlers.NotAnError)
	case "Regenerate explanation":
		call(w.handlers.Regenerate)
	}
}

// ---- Explanation panel (section 4.4) ----

// SetExplanation puts the panel into a state. header is the line above the
// explanation (the error type and its count) and markdown the body; the idle
// state ignores both and shows the hint.
func (w *MainWindow) SetExplanation(state view.ExplainState, header, markdown string) {
	w.explainState = state
	w.explainHeader = header
	switch {
	case state == view.ExplainIdle:
		markdown = w.hintText()
	case state == view.ExplainCancelled && markdown == "":
		markdown = "Explanation cancelled."
	}
	w.explainBody = markdown
	w.refresh()
}

// SetExplanationHeader replaces only the header line.
func (w *MainWindow) SetExplanationHeader(header string) { w.explainHeader = header; w.refresh() }

// SetExplanationMarkdown replaces the body while an explanation streams in. The
// view keeps following the end if it was at the end.
func (w *MainWindow) SetExplanationMarkdown(markdown string) {
	atEnd := w.explainAtEnd()
	w.explainBody = markdown
	w.refresh()
	if atEnd && w.rt != nil {
		w.rt.AfterFrame(w.scrollExplainToEnd)
	}
}

func (w *MainWindow) hintText() string {
	if len(w.llms) == 0 {
		return hintNoLLM
	}
	return hintIdle
}

// explainAtEnd reports whether the body is scrolled to its end (or does not
// scroll).
func (w *MainWindow) explainAtEnd() bool {
	if !w.explainScroll.HasClients() {
		return true
	}
	p := w.explainScroll.Position()
	return p.Pixels() >= p.MaxScrollExtent()-1
}

func (w *MainWindow) scrollExplainToEnd() {
	if w.explainScroll.HasClients() {
		w.explainScroll.JumpTo(w.explainScroll.Position().MaxScrollExtent())
	}
}

// ExplanationState returns the state of the panel.
func (w *MainWindow) ExplanationState() view.ExplainState { return w.explainState }

// ExplanationText returns the body of the panel as plain text.
func (w *MainWindow) ExplanationText() string { return plainText(parseMarkdown(w.explainBody)) }

// ExplanationHeader returns the header line, or "" if it is hidden.
func (w *MainWindow) ExplanationHeader() string {
	if !view.ControlsFor(w.explainState).Header {
		return ""
	}
	return w.explainHeader
}

// ExplanationButtons returns the label of the Regenerate / Retry button ("" if
// hidden) and whether the Cancel and Not an error buttons are shown.
func (w *MainWindow) ExplanationButtons() (redo string, cancel, notAnError bool) {
	c := view.ControlsFor(w.explainState)
	if c.Redo {
		redo = c.RedoLabel
	}
	return redo, c.Cancel, c.NotAnError
}

// ExplanationProgressShown reports whether the busy indicator is shown.
func (w *MainWindow) ExplanationProgressShown() bool {
	return view.ControlsFor(w.explainState).Progress
}

// ClickExplainButton presses one of the panel's buttons: "redo", "cancel" or
// "notanerror". A button that is hidden cannot be pressed.
func (w *MainWindow) ClickExplainButton(which string) {
	c := view.ControlsFor(w.explainState)
	switch {
	case which == "redo" && c.Redo:
		call(w.handlers.Regenerate)
	case which == "cancel" && c.Cancel:
		call(w.handlers.ExplainCancel)
	case which == "notanerror" && c.NotAnError:
		call(w.handlers.NotAnError)
	}
}

// ClickExplanationLink clicks a link in the panel, as the user would.
func (w *MainWindow) ClickExplanationLink(raw string) { w.onLink(raw) }

// SetLinkActions replaces what confirming and opening a link do. It exists so
// that tests never open a browser or wait for a dialog.
func (w *MainWindow) SetLinkActions(confirm func(url string) bool, open func(url string)) {
	w.confirmLink, w.openLink = confirm, open
}

// onLink handles a link of the explanation. They come from an LLM (section 5.9),
// so the window follows none by itself: DecideLink says what may happen.
func (w *MainWindow) onLink(raw string) {
	switch view.DecideLink(raw, w.explainState == view.ExplainIdle) {
	case view.LinkPreferences:
		call(w.handlers.Preferences)
	case view.LinkConfirm:
		if w.confirmLink(raw) {
			w.openLink(raw)
		}
	}
}

// linkRecognizer makes a recognizer for a link, to be disposed with the next
// build of the body. It is nil when the window has no runtime.
func (w *MainWindow) linkRecognizer(raw string) gestures.GestureRecognizer {
	if w.rt == nil {
		return nil
	}
	r := gestures.NewTapGestureRecognizer(w.rt.Binding().Gestures())
	r.OnTap = func() { w.onLink(raw) }
	w.recognizers = append(w.recognizers, r)
	return r
}

func (w *MainWindow) disposeRecognizers() {
	for _, r := range w.recognizers {
		r.Dispose()
	}
	w.recognizers = nil
}

// ShowHint shows the idle hint, or the no-LLM message if no LLM is configured.
func (w *MainWindow) ShowHint() { w.SetExplanation(view.ExplainIdle, "", "") }
