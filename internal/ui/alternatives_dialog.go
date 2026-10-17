package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"
)

// maxShownSelection is how much of the selected text the dialog repeats above
// the list; a long paragraph is cut with an ellipsis.
const maxShownSelection = 400

// AlternativesHooks lets the application react to what the user does in the
// Alternatives window. Every field is optional.
type AlternativesHooks struct {
	// Retry asks for the alternatives again, after a failure.
	Retry func()
	// Closed is called once, when the window is closed (by the user or by
	// Close), so that a running request can be stopped.
	Closed func()
}

// AlternativesDialog is the window that lists other ways to write the text the
// user selected in the editor (section 4.2). It is not modal: the user can keep
// working in the main window while it is open.
type AlternativesDialog struct {
	dlg   *qt.QDialog
	hooks AlternativesHooks

	selection *qt.QLabel
	status    *qt.QLabel
	bar       *qt.QProgressBar
	list      *qt.QListWidget
	copyBtn   *qt.QPushButton
	retryBtn  *qt.QPushButton
	banner    *qt.QLabel

	alternatives []string
	closed       bool

	// copyText puts text on the clipboard; tests replace it.
	copyText func(text string)
}

// NewAlternativesDialog builds the window for selected, in the loading state.
// It is not shown until Show is called.
func NewAlternativesDialog(parent *qt.QWidget, selected string, hooks AlternativesHooks) *AlternativesDialog {
	d := &AlternativesDialog{hooks: hooks}
	d.copyText = func(text string) { qt.QGuiApplication_Clipboard().SetText(text) }
	d.dlg = qt.NewQDialog(parent)
	d.dlg.SetWindowTitle("Alternatives")
	d.dlg.Resize(560, 420)

	layout := qt.NewQVBoxLayout2()
	d.dlg.SetLayout(layout.QLayout)

	layout.AddWidget(qt.NewQLabel3("Selected text:").QWidget)
	d.selection = qt.NewQLabel3(ShortSelection(selected))
	d.selection.SetWordWrap(true)
	d.selection.SetTextFormat(qt.PlainText)
	d.selection.SetTextInteractionFlags(qt.TextSelectableByMouse)
	d.selection.SetStyleSheet("padding: 6px; background: rgba(127, 127, 127, 40);")
	layout.AddWidget(d.selection.QWidget)

	d.status = qt.NewQLabel3("")
	d.status.SetStyleSheet("font-weight: bold;")
	layout.AddWidget(d.status.QWidget)
	d.bar = qt.NewQProgressBar2()
	d.bar.SetRange(0, 0) // busy indicator
	d.bar.SetTextVisible(false)
	d.bar.SetMaximumHeight(8)
	layout.AddWidget(d.bar.QWidget)

	d.list = qt.NewQListWidget2()
	d.list.SetWordWrap(true)
	d.list.SetTextElideMode(qt.ElideNone)
	d.list.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	d.list.SetVerticalScrollMode(qt.QAbstractItemView__ScrollPerPixel)
	d.list.SetAlternatingRowColors(true)
	d.list.SetStyleSheet("QListWidget::item { padding: 6px 4px; }")
	layout.AddWidget2(d.list.QWidget, 1)

	d.banner = qt.NewQLabel3("")
	d.banner.SetWordWrap(true)
	d.banner.SetTextInteractionFlags(qt.TextSelectableByMouse)
	d.banner.SetStyleSheet("color: " + colorError + ";")
	d.banner.Hide()
	layout.AddWidget(d.banner.QWidget)

	row := qt.NewQHBoxLayout2()
	d.copyBtn = qt.NewQPushButton3("Copy")
	d.copyBtn.SetToolTip("Copy the selected alternative to the clipboard (or double-click it)")
	d.retryBtn = qt.NewQPushButton3("Retry")
	closeBtn := qt.NewQPushButton3("Close")
	closeBtn.SetDefault(true)
	row.AddWidget(d.copyBtn.QWidget)
	row.AddWidget(d.retryBtn.QWidget)
	row.AddStretch()
	row.AddWidget(closeBtn.QWidget)
	layout.AddLayout(row.QLayout)

	d.copyBtn.OnClicked(d.copySelected)
	d.retryBtn.OnClicked(func() { call(d.hooks.Retry) })
	closeBtn.OnClicked(func() { d.dlg.Close() })
	d.list.OnItemDoubleClicked(func(*qt.QListWidgetItem) { d.copySelected() })
	d.list.OnCurrentRowChanged(func(int) { d.updateButtons() })
	d.dlg.OnFinished(func(int) { d.onClosed() })

	d.SetLoading()
	return d
}

// ShortSelection is the selected text as the dialog repeats it: a long one is
// cut to a few hundred characters and ends with an ellipsis.
func ShortSelection(s string) string {
	r := []rune(s)
	if len(r) <= maxShownSelection {
		return s
	}
	return string(r[:maxShownSelection]) + "…"
}

// AlternativesStatus is the line above the list for n alternatives.
func AlternativesStatus(n int) string {
	switch n {
	case 0:
		return "No alternatives found."
	case 1:
		return "1 alternative"
	default:
		return fmt.Sprintf("%d alternatives", n)
	}
}

// Show shows the window and brings it to the front.
func (d *AlternativesDialog) Show() {
	d.dlg.Show()
	d.dlg.Raise()
	d.dlg.ActivateWindow()
}

// Close closes the window as if the user had closed it.
func (d *AlternativesDialog) Close() { d.dlg.Close() }

// Dialog returns the dialog widget.
func (d *AlternativesDialog) Dialog() *qt.QDialog { return d.dlg }

func (d *AlternativesDialog) onClosed() {
	if d.closed {
		return
	}
	d.closed = true
	call(d.hooks.Closed)
}

// SetLoading shows that the alternatives are being fetched.
func (d *AlternativesDialog) SetLoading() {
	d.status.SetText("Looking for alternatives...")
	d.bar.Show()
	d.banner.SetText("")
	d.banner.Hide()
	d.list.Clear()
	d.alternatives = nil
	d.updateButtons()
}

// SetAlternatives shows the alternatives, one per row, and selects the first.
func (d *AlternativesDialog) SetAlternatives(alts []string) {
	d.alternatives = append([]string(nil), alts...)
	d.bar.Hide()
	d.banner.SetText("")
	d.banner.Hide()
	d.list.Clear()
	for _, a := range alts {
		d.list.AddItem(a)
	}
	d.status.SetText(AlternativesStatus(len(alts)))
	if len(alts) > 0 {
		d.list.SetCurrentRow(0)
	}
	d.updateButtons()
}

// SetFailed shows why the alternatives could not be fetched; Retry asks again.
func (d *AlternativesDialog) SetFailed(msg string) {
	d.alternatives = nil
	d.list.Clear()
	d.bar.Hide()
	d.status.SetText("Could not get alternatives.")
	d.banner.SetText(msg)
	d.banner.Show()
	d.updateButtons()
}

func (d *AlternativesDialog) updateButtons() {
	d.copyBtn.SetEnabled(d.currentRow() >= 0)
	// Retry is offered when there is something to retry: after a failure.
	d.retryBtn.SetVisible(!d.banner.IsHidden())
}

func (d *AlternativesDialog) currentRow() int {
	row := d.list.CurrentRow()
	if row < 0 || row >= len(d.alternatives) {
		return -1
	}
	return row
}

func (d *AlternativesDialog) copySelected() {
	if row := d.currentRow(); row >= 0 {
		d.copyText(d.alternatives[row])
	}
}

// The methods below let tests act as the user and read what the window shows.

// Alternatives returns the alternatives the list shows.
func (d *AlternativesDialog) Alternatives() []string { return append([]string(nil), d.alternatives...) }

// SelectionText returns the selected text as the window repeats it.
func (d *AlternativesDialog) SelectionText() string { return d.selection.Text() }

// StatusText returns the line above the list.
func (d *AlternativesDialog) StatusText() string { return d.status.Text() }

// Loading reports whether the busy indicator is shown.
func (d *AlternativesDialog) Loading() bool { return !d.bar.IsHidden() }

// ErrorText returns the failure message, or "" if there is none.
func (d *AlternativesDialog) ErrorText() string {
	if d.banner.IsHidden() {
		return ""
	}
	return d.banner.Text()
}

// RetryVisible reports whether the Retry button is shown.
func (d *AlternativesDialog) RetryVisible() bool { return !d.retryBtn.IsHidden() }

// CopyEnabled reports whether the Copy button can be pressed.
func (d *AlternativesDialog) CopyEnabled() bool { return d.copyBtn.IsEnabled() }

// SelectRow makes row the current one.
func (d *AlternativesDialog) SelectRow(row int) { d.list.SetCurrentRow(row) }

// CopyClick presses the Copy button.
func (d *AlternativesDialog) CopyClick() { d.copyBtn.Click() }

// RetryClick presses the Retry button.
func (d *AlternativesDialog) RetryClick() { d.retryBtn.Click() }

// SetCopyAction replaces what Copy does with the text, so that tests do not
// touch the clipboard.
func (d *AlternativesDialog) SetCopyAction(f func(text string)) { d.copyText = f }

// IsVisible reports whether the window is shown.
func (d *AlternativesDialog) IsVisible() bool { return d.dlg.IsVisible() }
