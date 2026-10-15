package app

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/uitest"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	return st
}

func addLLM(t *testing.T, st *store.Store, name string) uint {
	t.Helper()
	cfg := &store.LLMConfig{Name: name, Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
	if err := st.CreateLLMConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.ID
}

// withApp builds an App on the Qt thread and closes its window afterwards.
func withApp(t *testing.T, st *store.Store, f func(a *App, logs *syncBuffer)) {
	t.Helper()
	logs := &syncBuffer{}
	uitest.OnUI(func() {
		a := New(context.Background(), slog.New(slog.NewTextHandler(logs, nil)), st)
		defer a.win.Widget().Delete()
		f(a, logs)
	})
}

func TestNewAppWithFreshDatabase(t *testing.T) {
	st := newStore(t)
	withApp(t, st, func(a *App, logs *syncBuffer) {
		w := a.Window()
		if w.SelectedLLMID() != 0 {
			t.Error("no LLM is configured, so none can be selected")
		}
		if w.AutoCheck() {
			t.Error("auto-check must be off on a fresh database")
		}
		if w.FontSize() != store.DefaultFontSize {
			t.Errorf("font size = %d, want %d", w.FontSize(), store.DefaultFontSize)
		}
		if logs.String() != "" {
			t.Errorf("nothing should be logged on a clean start:\n%s", logs.String())
		}
	})
}

func TestNewAppLoadsSavedState(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	addLLM(t, st, "local")
	claude := addLLM(t, st, "claude")
	for _, err := range []error{
		st.SetSelectedLLMID(ctx, claude),
		st.SetAutoCheck(ctx, true),
		st.SetFontSize(ctx, 18),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	withApp(t, st, func(a *App, _ *syncBuffer) {
		w := a.Window()
		if w.SelectedLLMID() != claude {
			t.Errorf("selected LLM = %d, want %d", w.SelectedLLMID(), claude)
		}
		if !w.AutoCheck() {
			t.Error("auto-check should be restored")
		}
		if w.FontSize() != 18 {
			t.Errorf("font size = %d, want 18", w.FontSize())
		}
	})
}

func TestChangesAreSaved(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	addLLM(t, st, "local")
	claude := addLLM(t, st, "claude")

	withApp(t, st, func(a *App, _ *syncBuffer) {
		a.onLLMSelected(claude)
		a.onAutoCheckChanged(true)
	})
	if id, err := st.SelectedLLMID(ctx); err != nil || id != claude {
		t.Errorf("saved LLM = %d, %v", id, err)
	}
	if on, err := st.AutoCheck(ctx); err != nil || !on {
		t.Errorf("saved auto-check = %v, %v", on, err)
	}
}

func TestClosingSavesTheWindowState(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	var sizes []int
	withApp(t, st, func(a *App, _ *syncBuffer) {
		w := a.Window()
		a.Show()
		qt.QCoreApplication_ProcessEvents()
		w.SetSplitterSizes([]int{500, 250, 100, 300})
		qt.QCoreApplication_ProcessEvents()
		sizes = w.SplitterSizes()
		w.Close()
	})
	geometry, err := st.WindowGeometry(ctx)
	if err != nil || len(geometry) == 0 {
		t.Fatalf("geometry not saved: %v, %v", geometry, err)
	}
	saved, err := st.SplitterSizes(ctx)
	if err != nil || !slices.Equal(saved, sizes) {
		t.Fatalf("saved sizes = %v, %v; want %v", saved, err, sizes)
	}

	// The next start uses them.
	withApp(t, st, func(a *App, _ *syncBuffer) {
		a.Show()
		qt.QCoreApplication_ProcessEvents()
		got := a.Window().SplitterSizes()
		if len(got) != 4 {
			t.Fatalf("restored sizes = %v", got)
		}
		// The offscreen screen is smaller than the window was, so the window
		// comes back narrower: compare the proportions of the horizontal
		// splitter. Heights do not change.
		ratio := func(a, b int) float64 { return float64(a) / float64(b) }
		if d := ratio(got[0], got[1]) - ratio(sizes[0], sizes[1]); d < -0.1 || d > 0.1 {
			t.Errorf("horizontal sizes %v do not keep the proportions of %v", got[:2], sizes[:2])
		}
		if d := ratio(got[2], got[3]) - ratio(sizes[2], sizes[3]); d < -0.1 || d > 0.1 {
			t.Errorf("vertical sizes %v do not keep the proportions of %v", got[2:], sizes[2:])
		}
	})
}

func TestStartsWhenSettingsCannotBeRead(t *testing.T) {
	st := newStore(t)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	withApp(t, st, func(a *App, logs *syncBuffer) {
		w := a.Window()
		if w.SelectedLLMID() != 0 || w.AutoCheck() {
			t.Error("a broken database must give the defaults")
		}
		out := logs.String()
		for _, want := range []string{"reading LLM list", "reading auto-check setting", "DatabaseQueryFailed"} {
			if !strings.Contains(out, want) {
				t.Errorf("log missing %q:\n%s", want, out)
			}
		}
		// Saving fails too, and that must not crash.
		a.onLLMSelected(1)
		a.onAutoCheckChanged(true)
		a.saveWindowState()
		if !strings.Contains(logs.String(), "saving selected LLM") {
			t.Errorf("a failed save should be logged:\n%s", logs.String())
		}
	})
}
