package gui

import (
	"sync"
	"time"

	"github.com/rm4n0s/glutter/widgets"
)

// Runtime runs code on the UI thread. glutter's framework belongs to one
// goroutine; a background goroutine hands its result over with Post, and timers
// fire on the UI thread too. It stands in for Qt's mainthread package and QTimer.
//
// The binding exists only once the app runs, but the application is built before
// that, so a Runtime can be made empty and attached later; work asked for in the
// meantime waits for the attachment.
type Runtime struct {
	mu      sync.Mutex
	b       *widgets.WidgetsBinding
	pending []func(b *widgets.WidgetsBinding)
}

// NewRuntime makes the runtime of a binding: the one RunApp made, or a test's.
// Pass nil to attach it later with Attach.
func NewRuntime(b *widgets.WidgetsBinding) *Runtime { return &Runtime{b: b} }

// Attach gives the runtime its binding and runs the work that was waiting. It
// must be called on the UI thread.
func (r *Runtime) Attach(b *widgets.WidgetsBinding) {
	r.mu.Lock()
	if r.b != nil {
		r.mu.Unlock()
		return
	}
	r.b = b
	pending := r.pending
	r.pending = nil
	r.mu.Unlock()
	for _, f := range pending {
		f(b)
	}
}

// binding returns the binding, or queues f until there is one and returns nil.
func (r *Runtime) binding(f func(b *widgets.WidgetsBinding)) *widgets.WidgetsBinding {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.b == nil {
		r.pending = append(r.pending, f)
	}
	return r.b
}

// Post runs f on the UI thread. It may be called from any goroutine.
func (r *Runtime) Post(f func()) {
	if b := r.binding(func(b *widgets.WidgetsBinding) { b.Post(f) }); b != nil {
		b.Post(f)
	}
}

// After runs f on the UI thread after d, unless the returned stop function is
// called first. It must be called on the UI thread.
func (r *Runtime) After(d time.Duration, f func()) (stop func()) {
	var mu sync.Mutex
	var cancel func()
	stopped := false
	start := func(b *widgets.WidgetsBinding) {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			cancel = b.Gestures().NewTimer(d, f).Cancel
		}
	}
	if b := r.binding(start); b != nil {
		start(b)
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		stopped = true
		if cancel != nil {
			cancel()
		}
	}
}

// AfterFrame runs f once the frame that is going on, or the next one, is drawn.
// It must be called on the UI thread.
func (r *Runtime) AfterFrame(f func()) {
	run := func(b *widgets.WidgetsBinding) { b.RunPostFrame(func(time.Duration) { f() }) }
	if b := r.binding(run); b != nil {
		run(b)
	}
}

// Binding returns the widgets binding, or nil before the app runs.
func (r *Runtime) Binding() *widgets.WidgetsBinding {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b
}
