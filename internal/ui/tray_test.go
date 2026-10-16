package ui

import (
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/uitest"
)

func TestTrayMenu(t *testing.T) {
	uitest.OnUI(func() {
		var calls []string
		tray := NewTray(func() { calls = append(calls, "open") }, func() { calls = append(calls, "quit") })
		if got := tray.OpenAction().Text(); got != "Open" {
			t.Errorf("first item = %q, want Open", got)
		}
		if got := tray.CloseAction().Text(); got != "Close" {
			t.Errorf("second item = %q, want Close", got)
		}
		tray.OpenAction().Trigger()
		tray.CloseAction().Trigger()
		if want := []string{"open", "quit"}; len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
			t.Errorf("calls = %v, want %v", calls, want)
		}
	})
}

func TestTrayActivation(t *testing.T) {
	tests := []struct {
		name   string
		reason qt.QSystemTrayIcon__ActivationReason
		opens  int
	}{
		{"click", qt.QSystemTrayIcon__Trigger, 1},
		{"double click", qt.QSystemTrayIcon__DoubleClick, 1},
		{"middle click", qt.QSystemTrayIcon__MiddleClick, 0},
		{"context menu", qt.QSystemTrayIcon__Context, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.OnUI(func() {
				opened := 0
				quit := 0
				tray := NewTray(func() { opened++ }, func() { quit++ })
				tray.activated(tt.reason)
				if opened != tt.opens || quit != 0 {
					t.Errorf("opened %d (want %d), quit %d (want 0)", opened, tt.opens, quit)
				}
			})
		})
	}
}

func TestPresent(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *MainWindow)
	}{
		{"hidden", func(w *MainWindow) {}},
		{"closed by the user", func(w *MainWindow) { w.Show(); w.Close() }},
		{"minimised", func(w *MainWindow) { w.Show(); w.win.ShowMinimized() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withWindow(t, func(w *MainWindow) {
				tt.setup(w)
				w.Present()
				if !w.Visible() || w.win.IsMinimized() {
					t.Errorf("visible=%v minimised=%v after Present", w.Visible(), w.win.IsMinimized())
				}
			})
		})
	}
}
