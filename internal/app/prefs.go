package app

import (
	"context"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// preferences implements ui.PreferencesBackend on top of the store and the
// analysis package.
type preferences struct {
	store *store.Store
}

var _ ui.PreferencesBackend = preferences{}

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

func (p preferences) General(ctx context.Context) (ui.General, error) {
	var g ui.General
	var err error
	if g.AutoCheck, err = p.store.AutoCheck(ctx); err != nil {
		return ui.General{}, err
	}
	if g.AutoCheckDelayS, err = p.store.AutoCheckDelaySeconds(ctx); err != nil {
		return ui.General{}, err
	}
	if g.FontSize, err = p.store.FontSize(ctx); err != nil {
		return ui.General{}, err
	}
	return g, nil
}

func (p preferences) SetGeneral(ctx context.Context, g ui.General) error {
	if err := p.store.SetAutoCheck(ctx, g.AutoCheck); err != nil {
		return err
	}
	if err := p.store.SetAutoCheckDelaySeconds(ctx, g.AutoCheckDelayS); err != nil {
		return err
	}
	return p.store.SetFontSize(ctx, g.FontSize)
}

// openPreferences shows the modal Preferences dialog. When it changes
// something, the main window is refreshed straight away.
func (a *App) openPreferences() {
	dlg := ui.NewPreferencesDialog(a.ctx, a.win.Widget(), preferences{store: a.store}, ui.PreferencesHooks{
		Changed: a.applyPreferences,
		Failed:  func(err error) { logging.LogError(a.log, "preferences", err) },
	})
	dlg.Exec()
	dlg.Dialog().Delete()
}

// applyPreferences reloads what the Preferences dialog may have changed.
func (a *App) applyPreferences() {
	a.reloadLLMs()
	a.loadAutoCheck()
	if size, err := a.store.FontSize(a.ctx); err != nil {
		logging.LogError(a.log, "reading font size", err)
	} else {
		a.win.SetFontSize(size)
	}
}
