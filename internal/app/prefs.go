package app

import (
	"context"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// preferences implements gui.PreferencesBackend on top of the store and the
// analysis package.
type preferences struct {
	store *store.Store
}

var _ gui.PreferencesBackend = preferences{}

func (p preferences) ListLLMs(ctx context.Context) ([]store.LLMConfig, error) {
	return p.store.ListLLMConfigs(ctx)
}

func (p preferences) SaveLLM(ctx context.Context, cfg *store.LLMConfig) error {
	if cfg.ID == 0 {
		return p.store.CreateLLMConfig(ctx, cfg)
	}
	return p.store.UpdateLLMConfig(ctx, cfg)
}

func (p preferences) DeleteLLM(ctx context.Context, id uint) error {
	return p.store.DeleteLLMConfig(ctx, id)
}

func (p preferences) TestLLM(ctx context.Context, cfg store.LLMConfig) (analysis.TestResult, error) {
	return analysis.TestConnection(ctx, cfg.Name, cfg.ModelConfig(), 0)
}

func (p preferences) General(ctx context.Context) (gui.General, error) {
	var g gui.General
	var err error
	if g.AutoCheck, err = p.store.AutoCheck(ctx); err != nil {
		return gui.General{}, err
	}
	if g.AutoCheckDelayS, err = p.store.AutoCheckDelaySeconds(ctx); err != nil {
		return gui.General{}, err
	}
	if g.Fonts, err = p.store.Fonts(ctx); err != nil {
		return gui.General{}, err
	}
	return g, nil
}

func (p preferences) SetGeneral(ctx context.Context, g gui.General) error {
	if err := p.store.SetAutoCheck(ctx, g.AutoCheck); err != nil {
		return err
	}
	if err := p.store.SetAutoCheckDelaySeconds(ctx, g.AutoCheckDelayS); err != nil {
		return err
	}
	return p.store.SetFonts(ctx, g.Fonts)
}

// openPreferences shows the modal Preferences dialog. When it changes
// something, the main window is refreshed straight away.
func (a *App) openPreferences() {
	dlg := gui.NewPreferencesDialog(a.ctx, a.win, preferences{store: a.store}, gui.PreferencesHooks{
		Changed:    a.applyPreferences,
		Failed:     func(err error) { logging.LogError(a.log, "preferences", err) },
		WebToggle:  a.toggleWeb,
		WebState:   a.webState,
		WebChanged: a.onWebSettingsChanged,
		Closed:     func() { a.prefsDlg = nil },
	})
	a.prefsDlg = dlg
	a.win.ShowDialog(dlg)
}

// applyPreferences reloads what the Preferences dialog may have changed.
func (a *App) applyPreferences() {
	a.reloadLLMs()
	a.loadAutoCheck()
	a.loadFonts()
}

// loadFonts gives the window the stored fonts.
func (a *App) loadFonts() {
	if f, err := a.store.Fonts(a.ctx); err != nil {
		logging.LogError(a.log, "reading fonts", err)
	} else {
		a.win.SetFonts(f)
	}
}
