package gui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// splitter is two panes with a bar between them that the user drags, like Qt's
// QSplitter. Neither pane can be dragged smaller than minPane. Its sizes are
// kept in pixels, so they can be saved and restored.
type splitter struct {
	vertical bool // the panes are one above the other
	first    float64
	total    float64 // the room of the last layout, without the bar
	weight   [2]float64
	changed  func() // called when the user moves the bar
	notifier *foundation.ChangeNotifier
}

const (
	splitBar = 6.0  // thickness of the bar
	minPane  = 80.0 // the smallest a pane can get
)

// newSplitter makes a splitter whose panes share the room as weight[0]:weight[1]
// until the user or SetSizes says otherwise.
func newSplitter(vertical bool, w0, w1 float64) *splitter {
	return &splitter{vertical: vertical, weight: [2]float64{w0, w1}, notifier: foundation.NewChangeNotifier()}
}

// Sizes returns the sizes of the two panes in pixels, as of the last layout; it
// is empty before the first.
func (s *splitter) Sizes() []int {
	if s.total <= 0 {
		return nil
	}
	return []int{int(s.first + 0.5), int(s.total - s.first + 0.5)}
}

// SetSizes sets the share of the two panes.
func (s *splitter) SetSizes(sizes []int) {
	if len(sizes) != 2 || sizes[0] <= 0 || sizes[1] <= 0 {
		return
	}
	s.weight = [2]float64{float64(sizes[0]), float64(sizes[1])}
	s.first = 0
	s.total = 0
	s.notifier.NotifyListeners()
}

// Drag moves the bar by delta pixels, as the user dragging it would.
func (s *splitter) Drag(delta float64) {
	if s.total <= 0 {
		return
	}
	s.first = min(max(s.first+delta, minPane), max(s.total-minPane, minPane))
	if s.changed != nil {
		s.changed()
	}
	s.notifier.NotifyListeners()
}

// Widget lays out the two panes with the bar between them.
func (s *splitter) Widget(a, b widgets.Widget) widgets.Widget {
	return m.NewListenableBuilder(s.notifier, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		return m.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			room := c.MaxWidth
			if s.vertical {
				room = c.MaxHeight
			}
			room -= splitBar
			if room != s.total || s.first <= 0 {
				if s.total > 0 && s.first > 0 { // the window was resized: keep the share
					s.first = s.first / s.total * room
				} else {
					s.first = room * s.weight[0] / (s.weight[0] + s.weight[1])
				}
				s.total = room
			}
			s.first = min(max(s.first, min(minPane, room/2)), max(room-min(minPane, room/2), 0))
			return s.layout(bc, a, b)
		})
	}, nil)
}

func (s *splitter) layout(bc widgets.BuildContext, a, b widgets.Widget) widgets.Widget {
	theme := m.ThemeOf(bc)
	drag := m.NewMouseRegion(
		m.NewGestureDetector(m.NewColoredBox(theme.ColorScheme.OutlineVariant(), nil), m.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnPanUpdate: func(d gestures.DragUpdateDetails) {
				if s.vertical {
					s.Drag(d.Delta.Dy)
				} else {
					s.Drag(d.Delta.Dx)
				}
			},
		}),
		widgets.MouseRegionOpts{Cursor: cursorFor(s.vertical)})
	panes := func(w widgets.Widget, size float64) widgets.Widget {
		if s.vertical {
			return m.NewSizedBox(w, m.SizedBoxOpts{Height: foundation.Some(size)})
		}
		return m.NewSizedBox(w, m.SizedBoxOpts{Width: foundation.Some(size)})
	}
	bar := m.NewSizedBox(drag, sizedBar(s.vertical))
	children := []widgets.Widget{panes(a, s.first), bar, m.NewExpanded(b)}
	if s.vertical {
		return m.NewColumn(children)
	}
	return m.NewRow(children)
}

func sizedBar(vertical bool) m.SizedBoxOpts {
	if vertical {
		return m.SizedBoxOpts{Height: foundation.Some(splitBar)}
	}
	return m.SizedBoxOpts{Width: foundation.Some(splitBar)}
}

func cursorFor(vertical bool) services.MouseCursor {
	if vertical {
		return services.SystemMouseCursors.ResizeRow
	}
	return services.SystemMouseCursors.ResizeColumn
}
