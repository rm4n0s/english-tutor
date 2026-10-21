package gui

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// colorError is the colour of an error shown inside a dialog.
var colorError = ui.NewColor(0xFFD55E00)

// userMessage returns the text to show for an error: the message of an
// application error is written for the user, anything else is shown as it is.
func userMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// Dialog is the content of a modal dialog. The window shows it over itself; the
// dialog closes itself by calling the function it was given.
type Dialog interface {
	// Build returns the dialog's widget. close removes the dialog.
	Build(bc widgets.BuildContext, close func()) widgets.Widget
	// Dismissed is called once, when the dialog is gone, whoever closed it.
	Dismissed()
}

// dialogBase is what the dialogs have in common: they rebuild when their state
// changes and can close themselves.
type dialogBase struct {
	redraw *foundation.ChangeNotifier
	close  func()
	open   bool
}

func newDialogBase() dialogBase { return dialogBase{redraw: foundation.NewChangeNotifier()} }

func (b *dialogBase) refresh() { b.redraw.NotifyListeners() }

// Close removes the dialog from the screen, if it is on it.
func (b *dialogBase) Close() {
	if b.open && b.close != nil {
		b.close()
	}
}

// IsOpen reports whether the dialog is on the screen.
func (b *dialogBase) IsOpen() bool { return b.open }

// attach builds the widget of the dialog with build, which draws its current
// state.
func (b *dialogBase) attach(close func(), build func(bc widgets.BuildContext) widgets.Widget) widgets.Widget {
	b.close, b.open = close, true
	return m.NewListenableBuilder(b.redraw, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		return build(bc)
	}, nil)
}

// ShowDialog shows d over the window. It must be called on the UI thread, after
// the window is on screen.
func (w *MainWindow) ShowDialog(d Dialog) {
	if w.navCtx == nil {
		return
	}
	done := m.ShowDialog[any](w.navCtx, func(bc widgets.BuildContext) widgets.Widget {
		return d.Build(bc, func() { m.NavigatorPop(bc) })
	})
	go func() {
		<-done
		w.post(d.Dismissed)
	}()
}

// post runs f on the UI thread, now if there is no runtime to hand it to.
func (w *MainWindow) post(f func()) {
	if w.rt == nil {
		f()
		return
	}
	w.rt.Post(f)
}

// frame is the box every dialog is drawn in: a title, the body that takes the
// room, and a row of buttons.
func frame(bc widgets.BuildContext, title string, width, height float64, body widgets.Widget, actions ...widgets.Widget) widgets.Widget {
	theme := m.ThemeOf(bc)
	return m.NewDialog(m.NewSizedBox(m.NewPadding(painting.NewEdgeInsetsAll(16), m.NewColumn([]widgets.Widget{
		m.NewText(title, m.TextOpts{Style: theme.TextTheme.TitleLarge}),
		m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(12.0)}),
		m.NewExpanded(body),
		m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(12.0)}),
		m.NewRow(actions, m.RowOpts{MainAxisAlignment: m.MainAxisAlignmentEnd}),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})),
		m.SizedBoxOpts{Width: foundation.Some(width), Height: foundation.Some(height)}))
}

// errorLine is a message in the error colour, selectable so it can be copied.
func errorLine(bc widgets.BuildContext, text string) widgets.Widget {
	return m.NewSelectionArea(m.NewText(text, m.TextOpts{Style: m.ThemeOf(bc).TextTheme.BodyMedium.CopyWith(
		painting.TextStyleCopy{Color: foundation.Some(colorError)})}))
}

// button is a plain button of a dialog; a nil f disables it.
func button(label string, f func()) widgets.Widget { return m.NewOutlinedButton(f, m.NewText(label)) }

// primary is the button of the main action of a dialog.
func primary(label string, f func()) widgets.Widget { return m.NewFilledButton(f, m.NewText(label)) }

// bar is a space between buttons.
func spaceX(width float64) widgets.Widget {
	return m.NewSizedBox(nil, m.SizedBoxOpts{Width: foundation.Some(width)})
}

// listRow is one row of a list in a dialog: a tap selects it, a double tap
// activates it.
func listRow(bc widgets.BuildContext, text string, selected bool, onSelect, onActivate func()) widgets.Widget {
	var bg ui.Color
	if selected {
		bg = m.ThemeOf(bc).ColorScheme.PrimaryContainer()
	}
	return m.NewListener(m.NewGestureDetector(
		m.NewColoredBox(bg, m.NewPadding(painting.NewEdgeInsetsSymmetric(8, 10), m.NewText(text))),
		m.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnDoubleTap: onActivate}),
		widgets.ListenerOpts{OnPointerDown: func(*gestures.PointerDownEvent) { onSelect() }})
}
