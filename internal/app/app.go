// Package app ties the store, the analysis services and the window together.
package app

import (
	"context"
	"github.com/rm4n0s/english-tutor/internal/view"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/glutter/widgets"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/web"
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
	ctx       context.Context
	log       *slog.Logger
	store     *store.Store
	stats     *store.Stats
	analyzer  *analysis.Analyzer
	explainer *analysis.Explainer
	suggester *analysis.Suggester
	rt        *gui.Runtime
	win       *gui.MainWindow
	state     *issues.State

	// web is the web server (section 4.7). It runs only while the user wants it
	// to, and keeps no state of the window: every browser tab has its own.
	web *web.Server
	// prefsDlg is the open Preferences dialog, if any, so that it follows the
	// web server when the tray starts or stops it.
	prefsDlg *gui.PreferencesDialog
	// openBrowser opens an address in the browser; tests replace it.
	openBrowser func(url string)

	// tray is the system tray icon, nil when the application has none. With a
	// tray, closing the window only hides it; the tray's Close item quits.
	tray *gui.Tray
	// notifier shows a desktop message and windowActive says whether the user is
	// in the window. Tests replace them.
	notifier     func(title, text string)
	windowActive func() bool
	quit         func() // ends the event loop; tests replace it

	// newModel builds the langchaingo model of a configured LLM. Tests replace
	// it with a fake.
	newModel func(cfg store.LLMConfig) (llms.Model, error)

	// sourceKey identifies the text for the statistics (section 5.7); place says
	// where the text came from or was saved to.
	sourceKey string
	place     place
	dialogs   Dialogs

	// The fields below belong to the UI thread.
	checkSeq     uint64 // number of the latest check started
	checking     bool
	status       view.CheckStatus
	unplaced     int
	lastUsage    llm.Usage
	sessionUsage llm.Usage
	fromEditor   bool // a selection that came from the text cursor must not move it

	autoCheck bool
	autoDelay time.Duration
	autoTimer *timer

	// Explanation panel and issues list (UI thread only).
	explainSeq     uint64 // number of the latest explanation started
	explainBuf     strings.Builder
	explainDirty   bool
	explainHeader  string
	renderTimer    *timer
	resultRecorded bool           // the issues of the current result are in the statistics
	typeCounts     map[string]int // all-time mistakes per error type, for the row badges
	lastCheckID    uint64

	// The Alternatives window that is open, if any, and the number of the latest
	// request made for it (UI thread only).
	altDlg *gui.AlternativesDialog
	altSeq uint64

	// bgN counts the goroutines started by goBackground. A mutex and a
	// condition variable are used instead of a WaitGroup because goroutines are
	// started from UI callbacks while another goroutine may be waiting.
	bgMu   sync.Mutex
	bgN    int
	bgCond *sync.Cond
}

// New builds the window and loads the saved state into it. A setting that
// cannot be read is logged and replaced by its default, so the application
// still starts.
func New(ctx context.Context, log *slog.Logger, st *store.Store) *App {
	stats := store.NewStats(st, nil)
	rt := gui.NewRuntime(nil)
	a := &App{
		rt:         rt,
		ctx:        ctx,
		log:        log,
		store:      st,
		stats:      stats,
		analyzer:   analysis.NewAnalyzer(analysis.AnalyzerConfig{Usage: stats, Logger: log}),
		explainer:  analysis.NewExplainer(analysis.ExplainerConfig{Usage: stats, Logger: log}),
		suggester:  analysis.NewSuggester(analysis.SuggesterConfig{Usage: stats, Logger: log}),
		typeCounts: map[string]int{},
		win:        gui.NewMainWindow(rt),
		state:      issues.New(),
		sourceKey:  newSessionKey(),
		newModel: func(cfg store.LLMConfig) (llms.Model, error) {
			return llm.New(cfg.ModelConfig())
		},
		autoDelay: store.DefaultAutoCheckSec * time.Second,
	}
	a.quit = a.win.Exit
	a.openBrowser = a.win.OpenURL
	a.web = web.New(ctx, web.Deps{
		Log:   log,
		Store: st,
		Stats: stats,
		// Read when it is called, so that a test can replace a.newModel.
		NewModel: func(cfg store.LLMConfig) (llms.Model, error) { return a.newModel(cfg) },
		// The web page changed the LLM list or a setting: the window reloads them.
		SettingsChanged: func() { a.rt.Post(a.applyPreferences) },
	})
	a.notifier = a.trayNotify
	a.windowActive = a.win.IsActive
	a.autoTimer = newTimer(rt, a.onAutoCheckTimer)
	a.dialogs = windowDialogs{a: a}
	a.bgCond = sync.NewCond(&a.bgMu)
	a.renderTimer = newTimer(rt, a.onRenderTimer)
	a.state.Subscribe(a.onStateChange)
	a.win.SetHandlers(gui.Handlers{
		Check:            func() { a.startCheck(true) },
		Cancel:           a.analyzer.Cancel,
		Preferences:      a.openPreferences,
		New:              a.onNew,
		Open:             a.onOpen,
		Save:             a.onSave,
		SaveAs:           a.onSaveAs,
		SaveToLibrary:    a.onSaveToLibrary,
		Library:          a.onLibrary,
		CloseRequested:   a.onCloseRequested,
		ModifiedChanged:  func(bool) { a.updateTitle() },
		Statistics:       a.openStatistics,
		WebToggle:        a.toggleWeb,
		LLMSelected:      a.onLLMSelected,
		AutoCheckChanged: a.onAutoCheckChanged,
		CategoryToggled:  a.state.SetCategoryVisible,
		TextChanged:      a.onTextChanged,
		TextEdited: func(pos, removed, added int) {
			a.state.ApplyEdit(issues.Edit{Pos: pos, Removed: removed, Added: added})
		},
		CursorMoved:    a.onCursorMoved,
		TooltipAt:      a.tooltipAt,
		Alternative:    a.onAlternative,
		CheckSelection: a.onCheckSelection,
		IssueSelected:  func(id int) { a.state.Select(id) },
		IssueActivated: a.onIssueActivated,
		NotAnError:     a.onNotAnError,
		Regenerate:     a.onRegenerate,
		ExplainCancel:  a.explainer.Cancel,
		NextIssue:      func() { a.stepIssue(a.state.Next) },
		PrevIssue:      func() { a.stepIssue(a.state.Prev) },
	})
	a.loadState()
	a.refreshStatus()
	a.refreshList()
	a.updateTitle()
	return a
}

// goBackground runs f in a goroutine that Wait waits for. Goroutines must not
// touch the window; they post their result to the UI thread with rt.Post.
func (a *App) goBackground(f func()) {
	a.bgMu.Lock()
	a.bgN++
	a.bgMu.Unlock()
	go func() {
		defer func() {
			a.bgMu.Lock()
			a.bgN--
			if a.bgN == 0 {
				a.bgCond.Broadcast()
			}
			a.bgMu.Unlock()
		}()
		f()
	}()
}

// Wait blocks until the background goroutines have ended, so that nothing
// posts to the window after it is gone. Cancel the root context (and the
// analyzer) first, and do not call it on the UI thread. It does not run the
// callbacks the goroutines posted; after the event loop has stopped they never
// run, and in tests settle (see the test helpers) runs them.
func (a *App) Wait() {
	a.bgMu.Lock()
	defer a.bgMu.Unlock()
	for a.bgN > 0 {
		a.bgCond.Wait()
	}
}

// Show shows the main window.
func (a *App) Show() { a.win.Show() }

// Window returns the main window.
func (a *App) Window() *gui.MainWindow { return a.win }

func (a *App) loadState() {
	a.reloadLLMs()

	a.loadAutoCheck()
	a.loadFonts()
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
// LLM. If that LLM no longer exists (it was deleted), the selector falls back
// to another one, or to none, and the choice is remembered.
func (a *App) reloadLLMs() {
	cfgs, err := a.store.ListLLMConfigs(a.ctx)
	if err != nil {
		logging.LogError(a.log, "reading LLM list", err)
	}
	choices := make([]gui.LLMChoice, len(cfgs))
	for i, c := range cfgs {
		choices[i] = gui.LLMChoice{ID: c.ID, Name: c.Name}
	}
	selected, err := a.store.SelectedLLMID(a.ctx)
	if err != nil {
		logging.LogError(a.log, "reading selected LLM", err)
	}
	a.win.SetLLMs(choices, selected)
	if now := a.win.SelectedLLMID(); err == nil && now != selected {
		a.onLLMSelected(now)
	}
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
	a.setAutoCheck(on)
}

// loadAutoCheck reads the auto-check settings into the window and the timer.
func (a *App) loadAutoCheck() {
	on, err := a.store.AutoCheck(a.ctx)
	if err != nil {
		logging.LogError(a.log, "reading auto-check setting", err)
		on = false
	}
	if secs, err := a.store.AutoCheckDelaySeconds(a.ctx); err != nil {
		logging.LogError(a.log, "reading auto-check delay", err)
	} else {
		a.autoDelay = time.Duration(secs) * time.Second
	}
	a.win.SetAutoCheck(on)
	a.setAutoCheck(on)
}

// onCloseRequested is the close button of the window. With a tray icon the
// window is only hidden and the application, with any check that is running,
// goes on; the tray's Close item quits. Without one, closing the window ends the
// application, after the question about unsaved changes.
func (a *App) onCloseRequested() {
	if a.tray != nil {
		a.saveWindowState()
		a.win.Hide()
		return
	}
	a.confirmDiscard(func(ok bool) {
		if ok {
			a.finish()
		}
	})
}

// finish ends the application: what is running stops, the window state is saved
// and the event loop ends.
func (a *App) finish() {
	a.stopWork()
	a.saveWindowState()
	if a.tray != nil {
		a.tray.Hide()
	}
	a.quit()
}

// stopWork cancels what is running and stops the timers and the web server.
func (a *App) stopWork() {
	a.analyzer.Cancel()
	a.explainer.Cancel()
	a.suggester.Cancel()
	a.autoTimer.Stop()
	a.renderTimer.Stop()
	a.web.Stop()
}

// EnableTray puts the application in the system tray, so that closing the window
// hides it instead of quitting. The desktop may turn out to have no system tray;
// the application then behaves as if it had never asked for one (see Root).
func (a *App) EnableTray() bool {
	tray := gui.NewTray(gui.TrayActions{
		Open:      a.win.Present,
		Quit:      a.Quit,
		ToggleWeb: a.toggleWeb,
		OpenWeb:   a.openWebPage,
	})
	if !tray.Available() {
		return false
	}
	a.tray = tray
	tray.SetUnavailableHandler(func() {
		a.log.Info("no system tray available, closing the window quits the application")
		a.tray = nil
	})
	tray.SetWebRunning(a.web.Running())
	tray.Show()
	return true
}

// Root is the widget to run: the main window in its app, with the tray.
func (a *App) Root() widgets.Widget { return gui.Root(a.win, a.tray, a.rt) }

// TrayUnavailable tells the application that the desktop shows no tray icon.
func (a *App) TrayUnavailable() {
	if a.tray != nil {
		a.tray.MarkUnavailable()
	}
}

// Quit ends the application (the tray's Close item). Unsaved changes are asked
// about first, with the window brought to the front so that the question is
// seen; Cancel keeps the application running.
func (a *App) Quit() {
	if a.win.Modified() {
		a.win.Present()
	}
	a.confirmDiscard(func(ok bool) {
		if ok {
			a.finish()
		}
	})
}

func (a *App) saveWindowState() {
	if err := a.store.SetWindowGeometry(a.ctx, a.win.Geometry()); err != nil {
		logging.LogError(a.log, "saving window geometry", err)
	}
	if err := a.store.SetSplitterSizes(a.ctx, a.win.SplitterSizes()); err != nil {
		logging.LogError(a.log, "saving splitter sizes", err)
	}
}
