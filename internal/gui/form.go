package gui

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The controls the forms of the dialogs are made of.

// formRow is a label on the left and a control on the right.
func formRow(label string, control widgets.Widget) widgets.Widget {
	return m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0), m.NewRow([]widgets.Widget{
		m.NewSizedBox(m.NewText(label), m.SizedBoxOpts{Width: foundation.Some(150.0)}),
		m.NewExpanded(control),
	}, m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentCenter}))
}

// note is a line of small explanatory text under a control.
func note(bc widgets.BuildContext, text string, italic bool) widgets.Widget {
	style := m.ThemeOf(bc).TextTheme.BodySmall
	if italic {
		style = style.CopyWith(painting.TextStyleCopy{FontStyle: ui.FontStyleItalic})
	}
	return m.NewPadding(painting.NewEdgeInsetsSymmetric(2, 0), m.NewText(text, m.TextOpts{Style: style}))
}

// textInput is a one-line text field. A nil enabled-ness is on.
func textInput(c *widgets.TextEditingController, hint string, opts m.TextFieldOpts) widgets.Widget {
	opts.Controller = c
	opts.Decoration = ptr(m.NewInputDecoration(m.InputDecorationOpts{HintText: foundation.Some(hint), IsDense: foundation.Some(true)}))
	return m.NewTextField(opts)
}

// checkRow is a check box with its label; a nil onChanged disables it.
func checkRow(label string, value bool, onChanged func(bool)) widgets.Widget {
	var cb func(foundation.Opt[bool])
	if onChanged != nil {
		cb = func(v foundation.Opt[bool]) { onChanged(v.OrElse(false)) }
	}
	return m.NewRow([]widgets.Widget{m.NewCheckbox(foundation.Some(value), cb), m.NewText(label)}, m.RowOpts{MainAxisSize: m.MainAxisSizeMin})
}

// spin is a number with buttons that step it, like Qt's spin box. special is the
// text shown instead of min (empty for none).
type spin struct {
	value, min, max float64
	step            float64
	decimals        int
	suffix, special string
}

// text is the value as shown.
func (s spin) text() string {
	if s.special != "" && s.value == s.min {
		return s.special
	}
	return strconv.FormatFloat(s.value, 'f', s.decimals, 64) + s.suffix
}

// next is the value after a step, kept in the range.
func (s spin) next(delta float64) float64 {
	v := s.value + delta*s.step
	// Round away the error of adding 0.1 many times.
	p := 1.0
	for range s.decimals {
		p *= 10
	}
	v = float64(int(v*p+0.5*sign(v))) / p
	return min(max(v, s.min), s.max)
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func (s spin) widget(onChange func(float64)) widgets.Widget {
	var down, up func()
	if onChange != nil {
		down = func() { onChange(s.next(-1)) }
		up = func() { onChange(s.next(1)) }
	}
	return m.NewRow([]widgets.Widget{
		m.NewIconButton(down, m.NewText("−")),
		m.NewSizedBox(m.NewText(s.text(), m.TextOpts{TextAlign: ui.TextAlignCenter}), m.SizedBoxOpts{Width: foundation.Some(120.0)}),
		m.NewIconButton(up, m.NewText("+")),
	}, m.RowOpts{MainAxisSize: m.MainAxisSizeMin})
}
