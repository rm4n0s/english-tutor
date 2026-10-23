package app

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/english-tutor/internal/store"
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

// withApp builds an App on the test goroutine, which glutter's test binding takes
// as the UI thread, and disposes of its window afterwards.
func withApp(t *testing.T, st *store.Store, f func(a *App, logs *syncBuffer)) {
	t.Helper()
	withAppTester(t, st, func(a *App, logs *syncBuffer, _ *gluttertest.Tester) { f(a, logs) })
}

// withAppTester is withApp for a test that also needs the tester, to pump frames
// and find widgets.
func withAppTester(t *testing.T, st *store.Store, f func(a *App, logs *syncBuffer, tester *gluttertest.Tester)) {
	t.Helper()
	logs := &syncBuffer{}
	tester := gluttertest.New(t)
	a := New(context.Background(), slog.New(slog.NewTextHandler(logs, nil)), st)
	defer a.win.Dispose()
	tester.PumpWidget(a.Root())
	f(a, logs, tester)
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
		if w.Fonts() != store.DefaultFonts() {
			t.Errorf("fonts = %+v, want %+v", w.Fonts(), store.DefaultFonts())
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
	fonts := store.Fonts{Editor: store.Font{Family: "Test Serif", Size: 18}, Buttons: store.Font{Size: 10}, Results: store.Font{Family: "Test Sans", Size: 12}}
	for _, err := range []error{
		st.SetSelectedLLMID(ctx, claude),
		st.SetAutoCheck(ctx, true),
		st.SetFonts(ctx, fonts),
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
		if w.Fonts() != fonts {
			t.Errorf("fonts = %+v, want %+v", w.Fonts(), fonts)
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
	withAppTester(t, st, func(a *App, _ *syncBuffer, tester *gluttertest.Tester) {
		w := a.Window()
		a.Show()
		tester.Pump()
		w.SetSplitterSizes([]int{500, 250, 100, 300})
		tester.Pump()
		sizes = w.SplitterSizes()
		w.SetWindowSize(1000, 640)
		w.Close() // no tray: the application ends, with nothing to ask
		if !w.Quitting() {
			t.Error("closing the window without a tray ends the application")
		}
	})
	geometry, err := st.WindowGeometry(ctx)
	if err != nil || string(geometry) != "1000x640" {
		t.Fatalf("geometry saved: %q, %v", geometry, err)
	}
	saved, err := st.SplitterSizes(ctx)
	if err != nil || !slices.Equal(saved, sizes) {
		t.Fatalf("saved sizes = %v, %v; want %v", saved, err, sizes)
	}

	// The next start uses them. The size goes to the window that is opened, so it
	// is read before the test's own window of 800x600 lays the application out.
	logs := &syncBuffer{}
	tester := gluttertest.New(t)
	a := New(ctx, slog.New(slog.NewTextHandler(logs, nil)), st)
	defer a.win.Dispose()
	if width, height := a.Window().WindowSize(); width != 1000 || height != 640 {
		t.Errorf("window size = %dx%d", width, height)
	}
	tester.PumpWidget(a.Root())
	a.Show()
	tester.Pump()
	got := a.Window().SplitterSizes()
	if len(got) != 4 {
		t.Fatalf("restored sizes = %v", got)
	}
	// The window comes back at its size but the room of the panes differs a
	// little (the bars and the toolbar), so compare the proportions.
	ratio := func(a, b int) float64 { return float64(a) / float64(b) }
	if d := ratio(got[0], got[1]) - ratio(sizes[0], sizes[1]); d < -0.1 || d > 0.1 {
		t.Errorf("horizontal sizes %v do not keep the proportions of %v", got[:2], sizes[:2])
	}
	if d := ratio(got[2], got[3]) - ratio(sizes[2], sizes[3]); d < -0.1 || d > 0.1 {
		t.Errorf("vertical sizes %v do not keep the proportions of %v", got[2:], sizes[2:])
	}
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
