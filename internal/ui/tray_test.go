package ui

import (
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/uitest"
)

func TestTrayMenu(t *testing.T) {
	uitest.OnUI(func() {
		var calls []string
		tray := NewTray(TrayActions{
			Open:      func() { calls = append(calls, "open") },
			Quit:      func() { calls = append(calls, "quit") },
			ToggleWeb: func() { calls = append(calls, "web") },
			OpenWeb:   func() { calls = append(calls, "page") },
		})
		for _, c := range []struct {
			act  *qt.QAction
			want string
		}{
			{tray.OpenAction(), "Open"}, {tray.WebAction(), "Start web server"},
			{tray.OpenWebAction(), "Open web page"}, {tray.CloseAction(), "Close"},
		} {
			if got := c.act.Text(); got != c.want {
				t.Errorf("item = %q, want %q", got, c.want)
			}
		}
		tray.OpenAction().Trigger()
		tray.WebAction().Trigger()
		tray.CloseAction().Trigger()
		if want := []string{"open", "web", "quit"}; len(calls) != 3 || calls[0] != want[0] || calls[1] != want[1] || calls[2] != want[2] {
			t.Errorf("calls = %v, want %v", calls, want)
		}
	})
}

func TestTrayWebItemsFollowTheServer(t *testing.T) {
	uitest.OnUI(func() {
		var calls []string
		tray := NewTray(TrayActions{OpenWeb: func() { calls = append(calls, "page") }})
		if tray.OpenWebAction().IsEnabled() {
			t.Error("the page cannot be opened while the server is stopped")
		}
		tray.SetWebRunning(true)
		if got := tray.WebAction().Text(); got != "Stop web server" {
			t.Errorf("item = %q, want Stop web server", got)
		}
		if !tray.OpenWebAction().IsEnabled() {
			t.Error("the page can be opened while the server runs")
		}
		tray.OpenWebAction().Trigger()
		if len(calls) != 1 {
			t.Errorf("calls = %v", calls)
		}
		tray.SetWebRunning(false)
		if got := tray.WebAction().Text(); got != "Start web server" || tray.OpenWebAction().IsEnabled() {
			t.Errorf("after stopping: %q, open enabled %v", got, tray.OpenWebAction().IsEnabled())
		}
	})
}

func TestTrayMenuWithNoActionsDoesNothing(t *testing.T) {
	uitest.OnUI(func() {
		tray := NewTray(TrayActions{})
		tray.OpenAction().Trigger()
		tray.WebAction().Trigger()
		tray.CloseAction().Trigger()
		tray.activated(qt.QSystemTrayIcon__Trigger)
		tray.Notify("English Tutor", "ok") // must not crash, whether or not the desktop shows it
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
				tray := NewTray(TrayActions{Open: func() { opened++ }, Quit: func() { quit++ }})
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
