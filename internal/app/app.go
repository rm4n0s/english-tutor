// Package app ties the store, the analysis services and the Qt window together.
package app

import (
	"context"
	"log/slog"

	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// App is the running application: the main window and what it needs. It must
// be used on the UI thread.
//
// Settings are read and written synchronously on the UI thread. They are a few
// bytes in a local SQLite file, read before the window is shown and written when
// a control changes or the window closes. Anything that can be slow (LLM
// calls, statistics queries, saving documents) goes through goroutines in the
// later steps (section 6 of the spec).
type App struct {
	ctx   context.Context
	log   *slog.Logger
	store *store.Store
	win   *ui.MainWindow
}

// New builds the window and loads the saved state into it. A setting that
// cannot be read is logged and replaced by its default, so the application
// still starts.
func New(ctx context.Context, log *slog.Logger, st *store.Store) *App {
	a := &App{ctx: ctx, log: log, store: st, win: ui.NewMainWindow()}
	a.win.SetHandlers(ui.Handlers{
		LLMSelected:      a.onLLMSelected,
		AutoCheckChanged: a.onAutoCheckChanged,
		Closed:           a.saveWindowState,
	})
	a.loadState()
	return a
}

// Show shows the main window.
func (a *App) Show() { a.win.Show() }

// Window returns the main window.
func (a *App) Window() *ui.MainWindow { return a.win }

func (a *App) loadState() {
	a.reloadLLMs()

	if on, err := a.store.AutoCheck(a.ctx); err != nil {
		logging.LogError(a.log, "reading auto-check setting", err)
	} else {
		a.win.SetAutoCheck(on)
	}
	if size, err := a.store.FontSize(a.ctx); err != nil {
		logging.LogError(a.log, "reading font size", err)
	} else {
		a.win.SetFontSize(size)
	}
	if g, err := a.store.WindowGeometry(a.ctx); err != nil {
		logging.LogError(a.log, "reading window geometry", err)
	} else {
		a.win.SetGeometry(g)
	}
	if sizes, err := a.store.SplitterSizes(a.ctx); err != nil {
		logging.LogError(a.log, "reading splitter sizes", err)
	} else {
		a.win.SetSplitterSizes(sizes)
	}
}

// reloadLLMs refills the selector from the database and selects the remembered
// LLM.
func (a *App) reloadLLMs() {
	cfgs, err := a.store.ListLLMConfigs(a.ctx)
	if err != nil {
		logging.LogError(a.log, "reading LLM list", err)
	}
	choices := make([]ui.LLMChoice, len(cfgs))
	for i, c := range cfgs {
		choices[i] = ui.LLMChoice{ID: c.ID, Name: c.Name}
	}
	selected, err := a.store.SelectedLLMID(a.ctx)
	if err != nil {
		logging.LogError(a.log, "reading selected LLM", err)
	}
	a.win.SetLLMs(choices, selected)
}

func (a *App) onLLMSelected(id uint) {
	if err := a.store.SetSelectedLLMID(a.ctx, id); err != nil {
		logging.LogError(a.log, "saving selected LLM", err)
	}
}

func (a *App) onAutoCheckChanged(on bool) {
	if err := a.store.SetAutoCheck(a.ctx, on); err != nil {
		logging.LogError(a.log, "saving auto-check setting", err)
	}
}

func (a *App) saveWindowState() {
	if err := a.store.SetWindowGeometry(a.ctx, a.win.Geometry()); err != nil {
		logging.LogError(a.log, "saving window geometry", err)
	}
	if err := a.store.SetSplitterSizes(a.ctx, a.win.SplitterSizes()); err != nil {
		logging.LogError(a.log, "saving splitter sizes", err)
	}
}
