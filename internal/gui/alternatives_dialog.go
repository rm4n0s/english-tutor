package gui

import (
	"context"
	"fmt"

	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// maxShownSelection is how much of the selected text the dialog repeats above
// the list; a long paragraph is cut with an ellipsis.
const maxShownSelection = 400

// AlternativesHooks lets the application react to what the user does in the
// Alternatives window. Every field is optional.
type AlternativesHooks struct {
	// Retry asks for the alternatives again, after a failure.
	Retry func()
	// Chosen is called with the text of the alternative the user double-clicked.
	// The application puts it in the editor in place of the selection and closes
	// the window, or calls SetNotice to say why it could not.
	Chosen func(text string)
	// Closed is called once, when the window is closed (by the user or by
	// Close), so that a running request can be stopped.
	Closed func()
}

// AlternativesDialog lists other ways to write the text the user selected in the
// editor (section 4.2). It is not modal: it floats over a corner of the main
// window and the user can keep working while it is open.
type AlternativesDialog struct {
	dialogBase
	win   *MainWindow
	hooks AlternativesHooks

	selection string
	status    string
	loading   bool
	banner    string
	failed    bool // the banner shows a failure, so Retry is offered
	row       int
	closed    bool

	alternatives []string

	// copyText puts text on the clipboard; tests replace it.
	copyText func(text string)
}

// NewAlternativesDialog builds the window for selected, in the loading state.
// It is not shown until Show is called.
func NewAlternativesDialog(win *MainWindow, selected string, hooks AlternativesHooks) *AlternativesDialog {
	d := &AlternativesDialog{dialogBase: newDialogBase(), win: win, hooks: hooks, selection: ShortSelection(selected), row: -1}
	d.copyText = func(text string) {
		if win.rt != nil && win.rt.Binding() != nil {
			win.rt.Binding().Clipboard().SetData(context.Background(), services.NewClipboardData(text), func(err error) {
				if err != nil {
					win.ShowError("Could not copy: " + err.Error())
				}
			})
		}
	}
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

// Show shows the window over the main window.
func (d *AlternativesDialog) Show() { d.win.showPanel(d) }

// Close closes the window as if the user had closed it.
func (d *AlternativesDialog) Close() {
	if d.open {
		d.win.hidePanel(d)
	}
}

// IsVisible reports whether the window is shown.
func (d *AlternativesDialog) IsVisible() bool { return d.open }

func (d *AlternativesDialog) onClosed() {
	d.open = false
	if d.closed {
		return
	}
	d.closed = true
	call(d.hooks.Closed)
}

// SetLoading shows that the alternatives are being fetched.
func (d *AlternativesDialog) SetLoading() {
	d.failed, d.loading = false, true
	d.status = "Looking for alternatives..."
	d.banner = ""
	d.alternatives, d.row = nil, -1
	d.refresh()
}

// SetAlternatives shows the alternatives, one per row, and selects the first.
func (d *AlternativesDialog) SetAlternatives(alts []string) {
	d.alternatives = append([]string(nil), alts...)
	d.failed, d.loading = false, false
	d.banner = ""
	d.status = AlternativesStatus(len(alts))
	d.row = -1
	if len(alts) > 0 {
		d.row = 0
	}
	d.refresh()
}

// SetFailed shows why the alternatives could not be fetched; Retry asks again.
func (d *AlternativesDialog) SetFailed(msg string) {
	d.failed, d.loading = true, false
	d.alternatives, d.row = nil, -1
	d.status = "Could not get alternatives."
	d.banner = msg
	d.refresh()
}

// SetNotice shows a message under the list without touching the list, for
// something that went wrong with an alternative the user chose.
func (d *AlternativesDialog) SetNotice(msg string) { d.banner = msg; d.refresh() }

func (d *AlternativesDialog) currentRow() int {
	if d.row < 0 || d.row >= len(d.alternatives) {
		return -1
	}
	return d.row
}

// choose reports a double click on row.
func (d *AlternativesDialog) choose(row int) {
	if row >= 0 && row < len(d.alternatives) && d.hooks.Chosen != nil {
		d.hooks.Chosen(d.alternatives[row])
	}
}

func (d *AlternativesDialog) copySelected() {
	if row := d.currentRow(); row >= 0 {
		d.copyText(d.alternatives[row])
	}
}

// Build draws the window as a card.
func (d *AlternativesDialog) Build(bc widgets.BuildContext) widgets.Widget {
	return m.NewListenableBuilder(d.redraw, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		theme := m.ThemeOf(bc)
		var rows []widgets.Widget
		rows = append(rows,
			m.NewRow([]widgets.Widget{
				m.NewExpanded(m.NewText("Alternatives", m.TextOpts{Style: theme.TextTheme.TitleMedium})),
				tipped("Close", m.NewTextButton(d.Close, m.NewText("×"))),
			}),
			m.NewText("Selected text:"),
			m.NewContainer(m.NewSelectionArea(m.NewText(d.selection)), m.ContainerOpts{
				Padding: painting.NewEdgeInsetsAll(6), Color: foundation.Some(ui.NewColor(0x287F7F7F)),
			}),
			m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0), m.NewText(d.status, m.TextOpts{Style: theme.TextTheme.TitleSmall})))
		if d.loading {
			rows = append(rows, m.NewLinearProgressIndicator())
		}
		list := make([]widgets.Widget, len(d.alternatives))
		for i, a := range d.alternatives {
			list[i] = listRow(bc, a, i == d.row, func() { d.SelectRow(i) }, func() { d.DoubleClickRow(i) })
		}
		rows = append(rows, m.NewExpanded(m.NewSingleChildScrollView(m.NewColumn(list, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}))))
		if d.banner != "" {
			rows = append(rows, errorLine(bc, d.banner))
		}
		var onCopy func()
		if d.currentRow() >= 0 {
			onCopy = d.copySelected
		}
		buttons := []widgets.Widget{tipped("Copy the selected alternative to the clipboard. Double-click an alternative to use it in the text.",
			button("Copy", onCopy))}
		if d.failed {
			buttons = append(buttons, spaceX(8), button("Retry", func() { call(d.hooks.Retry) }))
		}
		rows = append(rows, m.NewRow(buttons))
		return m.NewMaterial(m.NewPadding(painting.NewEdgeInsetsAll(12), m.NewColumn(rows, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})),
			m.MaterialOpts{Elevation: 8, Color: foundation.Some(theme.ColorScheme.Surface()), BorderRadius: painting.NewBorderRadiusCircular(8)})
	}, nil)
}

func tipped(message string, child widgets.Widget) widgets.Widget {
	return m.NewTooltip(child, m.TooltipOpts{Message: message})
}

// The methods below let tests act as the user and read what the window shows.

// Alternatives returns the alternatives the list shows.
func (d *AlternativesDialog) Alternatives() []string { return append([]string(nil), d.alternatives...) }

// SelectionText returns the selected text as the window repeats it.
func (d *AlternativesDialog) SelectionText() string { return d.selection }

// StatusText returns the line above the list.
func (d *AlternativesDialog) StatusText() string { return d.status }

// Loading reports whether the busy indicator is shown.
func (d *AlternativesDialog) Loading() bool { return d.loading }

// ErrorText returns the failure message, or "" if there is none.
func (d *AlternativesDialog) ErrorText() string { return d.banner }

// RetryVisible reports whether the Retry button is shown.
func (d *AlternativesDialog) RetryVisible() bool { return d.failed }

// CopyEnabled reports whether the Copy button can be pressed.
func (d *AlternativesDialog) CopyEnabled() bool { return d.currentRow() >= 0 }

// DoubleClickRow double-clicks row, as the user would.
func (d *AlternativesDialog) DoubleClickRow(row int) { d.choose(row) }

// SelectRow makes row the current one.
func (d *AlternativesDialog) SelectRow(row int) {
	if row >= 0 && row < len(d.alternatives) {
		d.row = row
		d.refresh()
	}
}

// CopyClick presses the Copy button.
func (d *AlternativesDialog) CopyClick() { d.copySelected() }

// RetryClick presses the Retry button.
func (d *AlternativesDialog) RetryClick() {
	if d.failed {
		call(d.hooks.Retry)
	}
}

// SetCopyAction replaces what Copy does with the text, so that tests do not
// touch the clipboard.
func (d *AlternativesDialog) SetCopyAction(f func(text string)) { d.copyText = f }
