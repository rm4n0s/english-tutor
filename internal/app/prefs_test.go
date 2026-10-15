package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

func newConfig(name string) store.LLMConfig {
	return store.LLMConfig{Name: name, Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
}

func TestPreferencesBackendLLMs(t *testing.T) {
	ctx := context.Background()
	p := preferences{store: newStore(t)}

	first := newConfig("local")
	if err := p.SaveLLM(ctx, &first); err != nil || first.ID == 0 {
		t.Fatalf("create: %+v, %v", first, err)
	}
	first.Model = "other-model"
	if err := p.SaveLLM(ctx, &first); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := p.ListLLMs(ctx)
	if err != nil || len(list) != 1 || list[0].Model != "other-model" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	dup := newConfig("local")
	if err := p.SaveLLM(ctx, &dup); err == nil {
		t.Error("a duplicate name must be refused")
	}
	if err := p.DeleteLLM(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := p.ListLLMs(ctx); len(list) != 0 {
		t.Errorf("list after delete = %+v", list)
	}
}

func TestPreferencesBackendGeneral(t *testing.T) {
	ctx := context.Background()
	p := preferences{store: newStore(t)}
	got, err := p.General(ctx)
	if err != nil || got != (ui.General{AutoCheck: false, AutoCheckDelayS: 3, FontSize: 13}) {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	want := ui.General{AutoCheck: true, AutoCheckDelayS: 8, FontSize: 19}
	if err := p.SetGeneral(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, err := p.General(ctx); err != nil || got != want {
		t.Errorf("General() = %+v, %v; want %+v", got, err, want)
	}
}

func TestPreferencesBackendTestLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"ok"},"done":true}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()
	cfg := newConfig("local")
	cfg.BaseURL = srv.URL
	res, err := preferences{store: newStore(t)}.TestLLM(context.Background(), cfg)
	if err != nil || res.Cancelled || res.Elapsed <= 0 {
		t.Errorf("TestLLM() = %+v, %v", res, err)
	}
}

func TestDeletingTheSelectedLLMSelectsAnother(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	local := addLLM(t, st, "alpha")
	claude := addLLM(t, st, "beta")
	if err := st.SetSelectedLLMID(ctx, claude); err != nil {
		t.Fatal(err)
	}

	withApp(t, st, func(a *App, _ *syncBuffer) {
		if a.Window().SelectedLLMID() != claude {
			t.Fatalf("selected = %d, want %d", a.Window().SelectedLLMID(), claude)
		}
		if err := st.DeleteLLMConfig(ctx, claude); err != nil {
			t.Fatal(err)
		}
		a.applyPreferences()
		if a.Window().SelectedLLMID() != local {
			t.Errorf("after deleting the selected LLM, selected = %d, want %d", a.Window().SelectedLLMID(), local)
		}
		if id, _ := st.SelectedLLMID(ctx); id != local {
			t.Errorf("the new choice must be remembered, stored = %d", id)
		}

		if err := st.DeleteLLMConfig(ctx, local); err != nil {
			t.Fatal(err)
		}
		a.applyPreferences()
		if a.Window().SelectedLLMID() != 0 {
			t.Error("with no LLM left nothing is selected")
		}
		if id, _ := st.SelectedLLMID(ctx); id != 0 {
			t.Errorf("stored selection = %d, want 0", id)
		}
	})
}

func TestAddingAnLLMKeepsTheSelection(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	first := addLLM(t, st, "alpha")
	if err := st.SetSelectedLLMID(ctx, first); err != nil {
		t.Fatal(err)
	}
	withApp(t, st, func(a *App, _ *syncBuffer) {
		addLLM(t, st, "beta")
		a.applyPreferences()
		if a.Window().SelectedLLMID() != first {
			t.Errorf("selected = %d, want %d", a.Window().SelectedLLMID(), first)
		}
	})
}

func TestApplyPreferencesUpdatesTheWindow(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	withApp(t, st, func(a *App, _ *syncBuffer) {
		if err := (preferences{store: st}).SetGeneral(ctx, ui.General{AutoCheck: true, AutoCheckDelayS: 5, FontSize: 21}); err != nil {
			t.Fatal(err)
		}
		a.applyPreferences()
		if !a.Window().AutoCheck() || a.Window().FontSize() != 21 {
			t.Errorf("auto-check=%v font=%d", a.Window().AutoCheck(), a.Window().FontSize())
		}
	})
}

// TestPreferencesButtonOpensTheDialog clicks the toolbar button and closes the
// modal dialog from a timer that fires inside its event loop.
func TestPreferencesButtonOpensTheDialog(t *testing.T) {
	st := newStore(t)
	withApp(t, st, func(a *App, _ *syncBuffer) {
		opened := false
		timer := qt.NewQTimer()
		timer.SetSingleShot(true)
		timer.OnTimeout(func() {
			if w := qt.QApplication_ActiveModalWidget(); w != nil {
				opened = w.WindowTitle() == "Preferences"
				w.Close()
			}
		})
		timer.Start(0)
		a.win.PreferencesButtonClick()
		timer.Delete()
		if !opened {
			t.Error("the Preferences button should open the modal Preferences dialog")
		}
	})
}

// TestStatisticsButtonOpensTheDialog presses the toolbar button and closes the
// modal dialog from a timer that fires inside its event loop.
func TestStatisticsButtonOpensTheDialog(t *testing.T) {
	st := newStore(t)
	withApp(t, st, func(a *App, _ *syncBuffer) {
		opened := false
		timer := qt.NewQTimer()
		timer.SetSingleShot(true)
		timer.OnTimeout(func() {
			if w := qt.QApplication_ActiveModalWidget(); w != nil {
				opened = w.WindowTitle() == "Statistics"
				w.Close()
			}
		})
		timer.Start(0)
		a.win.StatisticsButtonClick()
		timer.Delete()
		if !opened {
			t.Error("the Statistics button should open the modal Statistics dialog")
		}
	})
}
