package app

import (
	"time"

	"github.com/rm4n0s/english-tutor/internal/gui"
)

// timer is a one-shot timer that fires on the UI thread, like Qt's QTimer with
// SetSingleShot. Starting it again restarts the wait.
type timer struct {
	rt     *gui.Runtime
	f      func()
	stop   func()
	active bool
}

func newTimer(rt *gui.Runtime, f func()) *timer { return &timer{rt: rt, f: f} }

// Start waits d and then calls f, in place of a wait that was running.
func (t *timer) Start(d time.Duration) {
	t.Stop()
	t.active = true
	t.stop = t.rt.After(d, func() {
		if !t.active {
			return
		}
		t.active = false
		t.f()
	})
}

// Stop cancels the wait, if there is one.
func (t *timer) Stop() {
	t.active = false
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
}

// IsActive reports whether a wait is running.
func (t *timer) IsActive() bool { return t.active }
