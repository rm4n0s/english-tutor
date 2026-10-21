package ui

import (
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"html"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/issues"
)

// buildExplainPanel creates the explanation panel (section 4.4): a header line,
// a busy indicator, the buttons, and the browser that shows the markdown.
func (w *MainWindow) buildExplainPanel() *qt.QWidget {
	panel := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout2()
	layout.SetContentsMargins(0, 0, 0, 0)
	panel.SetLayout(layout.QLayout)

	w.explainHeader = qt.NewQLabel3("")
	w.explainHeader.SetWordWrap(true)
	w.explainHeader.SetStyleSheet("font-weight: bold;")
	layout.AddWidget(w.explainHeader.QWidget)

	w.explainBar = qt.NewQProgressBar2()
	w.explainBar.SetRange(0, 0) // busy indicator
	w.explainBar.SetTextVisible(false)
	w.explainBar.SetMaximumHeight(8)
	layout.AddWidget(w.explainBar.QWidget)

	buttons := qt.NewQHBoxLayout2()
	w.redoBtn = qt.NewQPushButton3("Regenerate")
	w.cancelBtn = qt.NewQPushButton3("Cancel")
	w.notErrorBtn = qt.NewQPushButton3("Not an error")
	w.notErrorBtn.SetToolTip("The LLM was wrong: remove this issue and its count")
	buttons.AddWidget(w.redoBtn.QWidget)
	buttons.AddWidget(w.cancelBtn.QWidget)
	buttons.AddWidget(w.notErrorBtn.QWidget)
	buttons.AddStretch()
	layout.AddLayout(buttons.QLayout)

	w.explain = qt.NewQTextBrowser2()
	// Qt itself never opens a link: they come from an LLM (section 5.9) and go
	// through DecideLink. It never loads a resource either, so a remote or local
	// image written into an explanation is not fetched.
	w.explain.SetOpenLinks(false)
	w.explain.SetOpenExternalLinks(false)
	w.explain.OnLoadResource(func(func(int, *qt.QUrl) *qt.QVariant, int, *qt.QUrl) *qt.QVariant {
		return qt.NewQVariant()
	})
	layout.AddWidget2(w.explain.QWidget, 1)

	w.confirmLink = func(url string) bool {
		return qt.QMessageBox_Question2(w.win.QWidget, "Open link",
			"The explanation links to:\n\n"+url+"\n\nOpen it in your browser?",
			qt.QMessageBox__Yes, qt.QMessageBox__No) == int(qt.QMessageBox__Yes)
	}
	w.openLink = func(url string) { qt.QDesktopServices_OpenUrl(qt.NewQUrl3(url)) }
	return panel
}

// connectPanel connects the list and the panel.
func (w *MainWindow) connectPanel() {
	w.issuesList.OnCurrentRowChanged(func(row int) {
		if w.syncingList || row < 0 || row >= len(w.rowIDs) || w.handlers.IssueSelected == nil {
			return
		}
		w.handlers.IssueSelected(w.rowIDs[row])
	})
	w.issuesList.OnItemDoubleClicked(func(item *qt.QListWidgetItem) {
		row := w.issuesList.Row(item)
		if row >= 0 && row < len(w.rowIDs) && w.handlers.IssueActivated != nil {
			w.handlers.IssueActivated(w.rowIDs[row])
		}
	})
	w.issuesList.OnCustomContextMenuRequested(func(pos *qt.QPoint) {
		item := w.issuesList.ItemAt(pos)
		if item == nil || w.issuesList.Row(item) >= len(w.rowIDs) {
			return
		}
		w.issuesList.SetCurrentRow(w.issuesList.Row(item))
		w.buildIssueMenu().ExecWithPos(w.issuesList.Viewport().MapToGlobalWithQPoint(pos))
	})
	w.redoBtn.OnClicked(func() { call(w.handlers.Regenerate) })
	w.cancelBtn.OnClicked(func() { call(w.handlers.ExplainCancel) })
	w.notErrorBtn.OnClicked(func() { call(w.handlers.NotAnError) })
	w.explain.OnAnchorClicked(func(u *qt.QUrl) { w.onLink(u.ToString()) })
}

// buildIssueMenu is the right-click menu of a row (section 4.3).
func (w *MainWindow) buildIssueMenu() *qt.QMenu {
	menu := qt.NewQMenu(w.issuesList.QWidget)
	notError := menu.AddActionWithText("Not an error")
	notError.OnTriggered(func() { call(w.handlers.NotAnError) })
	regenerate := menu.AddActionWithText("Regenerate explanation")
	regenerate.OnTriggered(func() { call(w.handlers.Regenerate) })
	return menu
}

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

// SetExplanation puts the panel into a state. header is the line above the
// explanation (the error type and its count) and markdown the body; the idle
// state ignores both and shows the hint.
func (w *MainWindow) SetExplanation(state view.ExplainState, header, markdown string) {
	w.explainState = state
	c := view.ControlsFor(state)
	w.explainHeader.SetText(header)
	w.explainHeader.SetVisible(c.Header)
	w.explainBar.SetVisible(c.Progress)
	w.cancelBtn.SetVisible(c.Cancel)
	w.redoBtn.SetVisible(c.Redo)
	if c.Redo {
		w.redoBtn.SetText(c.RedoLabel)
	}
	w.notErrorBtn.SetVisible(c.NotAnError)

	switch {
	case state == view.ExplainIdle:
		markdown = w.hintText()
	case state == view.ExplainCancelled && markdown == "":
		markdown = "Explanation cancelled."
	}
	w.explain.SetMarkdown(markdown)
}

// SetExplanationHeader replaces only the header line.
func (w *MainWindow) SetExplanationHeader(header string) { w.explainHeader.SetText(header) }

// SetExplanationMarkdown replaces the body while an explanation streams in. The
// view keeps following the end if it was at the end.
func (w *MainWindow) SetExplanationMarkdown(markdown string) {
	bar := w.explain.VerticalScrollBar()
	atEnd := bar.Value() >= bar.Maximum()
	w.explain.SetMarkdown(markdown)
	if atEnd {
		bar.SetValue(bar.Maximum())
	}
}

func (w *MainWindow) hintText() string {
	if len(w.llms) == 0 {
		return hintNoLLM
	}
	return hintIdle
}

// SetIssues fills the issues list (section 4.3). With no rows the list shows
// emptyText as one grey line. selectedID is the row to select (0 for none);
// selecting from code reports nothing.
func (w *MainWindow) SetIssues(header string, rows []issues.Row, selectedID int, emptyText string) {
	w.syncingList = true
	defer func() { w.syncingList = false }()

	w.issuesHeader.SetText(header)
	w.issuesList.Clear()
	w.rowIDs = nil
	if len(rows) == 0 {
		w.issuesList.AddItem(emptyText)
		if item := w.issuesList.Item(0); item != nil {
			item.SetFlags(qt.NoItemFlags)
		}
		return
	}
	dark := w.isDark()
	selectedRow := -1
	for i, r := range rows {
		item := qt.NewQListWidgetItem()
		w.issuesList.AddItemWithItem(item)
		label := qt.NewQLabel3(rowHTML(r, dark))
		label.SetTextFormat(qt.RichText)
		label.SetAttribute(qt.WA_TransparentForMouseEvents)
		w.issuesList.SetItemWidget(item, label.QWidget)
		item.SetSizeHint(label.SizeHint())
		w.rowIDs = append(w.rowIDs, r.ID)
		if r.ID == selectedID {
			selectedRow = i
		}
	}
	w.issuesList.SetCurrentRow(selectedRow)
	if selectedRow >= 0 {
		w.issuesList.ScrollToItem(w.issuesList.Item(selectedRow))
	}
}

// rowHTML is the text of one list row: the category letter in the category
// colour, the snippet, and on a second line the error type and the count badge.
func rowHTML(r issues.Row, dark bool) string {
	badge := ""
	if b := r.Badge(); b != "" {
		badge = "&nbsp;&nbsp;<b>" + html.EscapeString(b) + "</b>"
	}
	return fmt.Sprintf(`<span style="color:%s; font-weight:bold">%s</span>&nbsp;%s<br/><span style="color:gray">%s%s</span>`,
		view.CategoryColor(r.Category, dark), html.EscapeString(r.Marker), html.EscapeString(r.Snippet),
		html.EscapeString(r.Label), badge)
}

// The methods below let tests of the code that sets the handlers act as the user
// and read what the panel and the list show.

// IssuesHeader returns the title above the list.
func (w *MainWindow) IssuesHeader() string { return w.issuesHeader.Text() }

// IssueRowIDs returns the issue ID of each row (empty if the list shows its
// empty line).
func (w *MainWindow) IssueRowIDs() []int { return append([]int(nil), w.rowIDs...) }

// IssueRowText returns the rich text of a row.
func (w *MainWindow) IssueRowText(row int) string {
	item := w.issuesList.Item(row)
	if item == nil {
		return ""
	}
	widget := w.issuesList.ItemWidget(item)
	if widget == nil {
		return item.Text()
	}
	return qt.UnsafeNewQLabel(widget.UnsafePointer()).Text()
}

// IssuesEmptyText returns the grey line shown instead of rows, or "".
func (w *MainWindow) IssuesEmptyText() string {
	if len(w.rowIDs) == 0 && w.issuesList.Count() == 1 {
		return w.issuesList.Item(0).Text()
	}
	return ""
}

// SelectedIssueRow returns the ID of the selected row, or 0.
func (w *MainWindow) SelectedIssueRow() int {
	row := w.issuesList.CurrentRow()
	if row < 0 || row >= len(w.rowIDs) {
		return 0
	}
	return w.rowIDs[row]
}

// ClickIssueRow selects a row as the user would with the mouse or the keys.
func (w *MainWindow) ClickIssueRow(row int) { w.issuesList.SetCurrentRow(row) }

// DoubleClickIssueRow double-clicks a row.
func (w *MainWindow) DoubleClickIssueRow(row int) {
	if item := w.issuesList.Item(row); item != nil {
		w.issuesList.ItemDoubleClicked(item)
	}
}

// ExplanationState returns the state of the panel.
func (w *MainWindow) ExplanationState() view.ExplainState { return w.explainState }

// ExplanationText returns the body of the panel as plain text.
func (w *MainWindow) ExplanationText() string { return w.explain.ToPlainText() }

// ExplanationHeader returns the header line, or "" if it is hidden.
func (w *MainWindow) ExplanationHeader() string {
	if w.explainHeader.IsHidden() {
		return ""
	}
	return w.explainHeader.Text()
}

// ExplanationButtons returns the label of the Regenerate / Retry button ("" if
// hidden) and whether the Cancel and Not an error buttons are shown.
func (w *MainWindow) ExplanationButtons() (redo string, cancel, notAnError bool) {
	if !w.redoBtn.IsHidden() {
		redo = w.redoBtn.Text()
	}
	return redo, !w.cancelBtn.IsHidden(), !w.notErrorBtn.IsHidden()
}

// ExplanationProgressShown reports whether the busy indicator is shown.
func (w *MainWindow) ExplanationProgressShown() bool { return !w.explainBar.IsHidden() }

// ClickExplainButton presses one of the panel's buttons: "redo", "cancel" or
// "notanerror".
func (w *MainWindow) ClickExplainButton(which string) {
	switch which {
	case "redo":
		w.redoBtn.Click()
	case "cancel":
		w.cancelBtn.Click()
	case "notanerror":
		w.notErrorBtn.Click()
	}
}

// ClickExplanationLink clicks a link in the panel, as the user would.
func (w *MainWindow) ClickExplanationLink(raw string) { w.onLink(raw) }

// SetLinkActions replaces what confirming and opening a link do. It exists so
// that tests never open a browser or wait for a dialog.
func (w *MainWindow) SetLinkActions(confirm func(url string) bool, open func(url string)) {
	w.confirmLink, w.openLink = confirm, open
}

// IssueMenuLabels returns the labels of the row context menu.
func (w *MainWindow) IssueMenuLabels() []string {
	menu := w.buildIssueMenu()
	defer menu.Delete()
	var out []string
	for _, a := range menu.Actions() {
		out = append(out, a.Text())
	}
	return out
}

// TriggerIssueMenu triggers one entry of the row context menu by its label.
func (w *MainWindow) TriggerIssueMenu(label string) {
	menu := w.buildIssueMenu()
	defer menu.Delete()
	for _, a := range menu.Actions() {
		if a.Text() == label {
			a.Trigger()
		}
	}
}
