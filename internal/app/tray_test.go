package app

import (
	"testing"

	"github.com/rm4n0s/english-tutor/internal/ui"
)

// withTray gives the harness a tray icon and a Quit that only counts, because
// the real one would stop the event loop the tests run on.
func withTray(h *harness) *int {
	quits := new(int)
	onUI(func() {
		h.a.quit = func() { *quits++ }
		h.a.tray = ui.NewTray(ui.TrayActions{Open: h.a.win.Present, Quit: h.a.Quit})
	})
	return quits
}

func TestClosingTheWindowWithATrayOnlyHidesIt(t *testing.T) {
	h, fd := newSavingHarness(t)
	quits := withTray(h)
	h.typed("draft")
	onUI(func() {
		h.a.Show()
		h.a.win.Close()
		if h.a.win.Visible() {
			t.Error("the window must be hidden")
		}
		if len(fd.unsavedAsked) != 0 {
			t.Errorf("hiding must not ask about unsaved changes, asked %v", fd.unsavedAsked)
		}
		if !h.a.win.Modified() || h.a.win.Text() != "draft" {
			t.Errorf("the text must stay as it was: %q modified=%v", h.a.win.Text(), h.a.win.Modified())
		}
		h.a.tray.OpenAction().Trigger()
		if !h.a.win.Visible() || h.a.win.Text() != "draft" {
			t.Errorf("Open must bring the same window back: visible=%v text=%q", h.a.win.Visible(), h.a.win.Text())
		}
	})
	if *quits != 0 {
		t.Errorf("closing the window quit the application %d times", *quits)
	}
}

func TestClosingTheWindowWithATraySavesItsState(t *testing.T) {
	h, _ := newSavingHarness(t)
	withTray(h)
	onUI(func() {
		h.a.Show()
		h.a.win.Close()
	})
	if g, err := h.st.WindowGeometry(h.a.ctx); err != nil || len(g) == 0 {
		t.Errorf("geometry = %v, %v; a hidden window's state is saved", g, err)
	}
}

func TestClosingTheWindowWithoutATrayStillQuits(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.unsaved = UnsavedCancel
	h.typed("draft")
	onUI(func() {
		h.a.Show()
		h.a.win.Close()
		if !h.a.win.Visible() || len(fd.unsavedAsked) != 1 {
			t.Errorf("without a tray the unsaved question is asked and Cancel keeps the window: visible=%v asked=%v",
				h.a.win.Visible(), fd.unsavedAsked)
		}
	})
}

func TestTrayCloseQuits(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		choice   UnsavedChoice
		asks     int
		wantQuit int
	}{
		{"unchanged text", "", UnsavedCancel, 0, 1},
		{"discard", "draft", UnsavedDiscard, 1, 1},
		{"cancel keeps the application", "draft", UnsavedCancel, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fd := newSavingHarness(t)
			fd.unsaved = tt.choice
			quits := withTray(h)
			if tt.text != "" {
				h.typed(tt.text)
			}
			onUI(func() {
				h.a.Show()
				h.a.win.Close() // hidden in the tray
				h.a.tray.CloseAction().Trigger()
				if len(fd.unsavedAsked) != tt.asks {
					t.Errorf("unsaved questions = %v, want %d", fd.unsavedAsked, tt.asks)
				}
				if tt.asks > 0 && !h.a.win.Visible() {
					t.Error("the window must be shown for the unsaved question")
				}
			})
			if *quits != tt.wantQuit {
				t.Errorf("quit %d times, want %d", *quits, tt.wantQuit)
			}
		})
	}
}

func TestTrayCloseSavesBeforeQuitting(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.unsaved, fd.savePath, fd.saveOK = UnsavedSave, t.TempDir()+"/out.txt", true
	quits := withTray(h)
	h.typed("draft")
	onUI(func() { h.a.tray.CloseAction().Trigger() })
	if *quits != 1 || len(fd.saveSuggestions) != 1 {
		t.Errorf("quits %d, save dialogs %v; Save writes the text and then quits", *quits, fd.saveSuggestions)
	}
	if g, err := h.st.WindowGeometry(h.a.ctx); err != nil || len(g) == 0 {
		t.Errorf("geometry = %v, %v; the window state is saved when quitting", g, err)
	}
}

func TestEnableTrayWithoutASystemTray(t *testing.T) {
	h, _ := newSavingHarness(t)
	onUI(func() {
		if ui.TrayAvailable() {
			t.Skip("this platform has a system tray")
		}
		if h.a.EnableTray() || h.a.tray != nil {
			t.Error("without a system tray the application must not hide its window")
		}
	})
}
