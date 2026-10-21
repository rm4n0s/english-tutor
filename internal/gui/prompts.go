package gui

import (
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/widgets"
)

// The questions the application asks the user. Each one is a small modal dialog
// and reports the answer to a function: glutter has no nested event loop, so a
// question cannot block the code that asked it.

// choiceDialog asks a question with buttons; the one the user presses is
// reported with its index. Closing the dialog any other way reports cancel.
type choiceDialog struct {
	dialogBase
	title, text string
	labels      []string
	cancel      int // the answer when the dialog is dismissed
	done        func(choice int)
	answered    bool
}

func (d *choiceDialog) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return d.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		var actions []widgets.Widget
		for i, label := range d.labels {
			if i > 0 {
				actions = append(actions, spaceX(8))
			}
			if i == 0 {
				actions = append(actions, primary(label, func() { d.answer(i) }))
			} else {
				actions = append(actions, button(label, func() { d.answer(i) }))
			}
		}
		return frame(bc, d.title, 420, 200, m.NewSelectionArea(m.NewText(d.text)), actions...)
	})
}

func (d *choiceDialog) answer(i int) {
	if d.answered {
		return
	}
	d.answered = true
	d.done(i)
	d.Close()
}

func (d *choiceDialog) Dismissed() {
	d.open = false
	if !d.answered {
		d.answered = true
		d.done(d.cancel)
	}
}

// Choose asks title/text with buttons labelled labels and reports the index of the
// one pressed, or cancel when the dialog is dismissed.
func (w *MainWindow) Choose(title, text string, labels []string, cancel int, done func(choice int)) *choiceDialog {
	d := &choiceDialog{dialogBase: newDialogBase(), title: title, text: text, labels: labels, cancel: cancel, done: done}
	w.ShowDialog(d)
	return d
}

// Confirm asks a yes/no question and reports true for Yes.
func (w *MainWindow) Confirm(title, text string, done func(yes bool)) {
	w.Choose(title, text, []string{"Yes", "No"}, 1, func(i int) { done(i == 0) })
}

// textDialog asks for a line of text.
type textDialog struct {
	dialogBase
	title, label string
	field        *widgets.TextEditingController
	focus        *widgets.FocusNode
	done         func(text string, ok bool)
	answered     bool
}

func (d *textDialog) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return d.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		input := m.NewTextField(m.TextFieldOpts{
			Controller: d.field, FocusNode: d.focus, Autofocus: true,
			Decoration:  ptr(m.NewInputDecoration(m.InputDecorationOpts{LabelText: foundation.Some(d.label)})),
			OnSubmitted: func(string) { d.answer(true) },
		})
		return frame(bc, d.title, 420, 190, input,
			button("Cancel", func() { d.answer(false) }), spaceX(8), primary("OK", func() { d.answer(true) }))
	})
}

func (d *textDialog) answer(ok bool) {
	if d.answered {
		return
	}
	d.answered = true
	d.done(d.field.Text(), ok)
	d.Close()
}

func (d *textDialog) Dismissed() {
	d.open = false
	if !d.answered {
		d.answered = true
		d.done(d.field.Text(), false)
	}
}

// AskText asks for a line of text, starting from initial.
func (w *MainWindow) AskText(title, label, initial string, done func(text string, ok bool)) *textDialog {
	d := &textDialog{dialogBase: newDialogBase(), title: title, label: label,
		field: widgets.NewTextEditingController(initial), focus: widgets.NewFocusNode(), done: done}
	w.ShowDialog(d)
	return d
}

func ptr[T any](v T) *T { return &v }
