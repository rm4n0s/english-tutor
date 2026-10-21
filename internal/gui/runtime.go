package gui

import (
	"time"

	"github.com/rm4n0s/glutter/widgets"
)

// Runtime runs code on the UI thread. glutter's framework belongs to one
// goroutine; a background goroutine hands its result over with Post, and timers
// fire on the UI thread too. It stands in for Qt's mainthread package and QTimer.
type Runtime struct{ b *widgets.WidgetsBinding }

// NewRuntime makes the runtime of a binding: the one RunApp made, or a test's.
func NewRuntime(b *widgets.WidgetsBinding) *Runtime { return &Runtime{b: b} }

// Post runs f on the UI thread. It may be called from any goroutine.
func (r *Runtime) Post(f func()) { r.b.Post(f) }

// After runs f on the UI thread after d, unless the returned stop function is
// called first. It must be called on the UI thread.
func (r *Runtime) After(d time.Duration, f func()) (stop func()) {
	t := r.b.Gestures().NewTimer(d, f)
	return t.Cancel
}

// AfterFrame runs f once the frame that is going on, or the next one, is drawn.
// It must be called on the UI thread.
func (r *Runtime) AfterFrame(f func()) { r.b.RunPostFrame(func(time.Duration) { f() }) }

// Binding returns the widgets binding.
func (r *Runtime) Binding() *widgets.WidgetsBinding { return r.b }
