package ui

import (
	"context"
	"fmt"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// General holds the settings of the General tab.
type General struct {
	AutoCheck       bool
	AutoCheckDelayS int
	FontSize        int
}

// PreferencesBackend is what the Preferences dialog needs from the rest of the
// application. internal/app implements it; tests use a fake.
//
// List, save, delete and the General settings are a few rows in a local file
// and are called on the UI thread. TestLLM is a network request and is always
// called from a goroutine.
type PreferencesBackend interface {
	ListLLMs(ctx context.Context) ([]store.LLMConfig, error)
	// SaveLLM creates the configuration if cfg.ID is 0 and updates it otherwise.
	SaveLLM(ctx context.Context, cfg *store.LLMConfig) error
	DeleteLLM(ctx context.Context, id uint) error
	TestLLM(ctx context.Context, cfg store.LLMConfig) (analysis.TestResult, error)
	General(ctx context.Context) (General, error)
	SetGeneral(ctx context.Context, g General) error
	// WebSettings, SetWebPort and SetWebPassword are the Web server tab. The
	// password is stored as a hash; an empty one removes it.
	WebSettings(ctx context.Context) (WebSettings, error)
	SetWebPort(ctx context.Context, port int) error
	SetWebPassword(ctx context.Context, password string) error
}

// PreferencesHooks lets the application react to the dialog.
type PreferencesHooks struct {
	// Changed is called after the LLM list or a general setting changed.
	Changed func()
	// Failed is called with every error the dialog shows, so it can be logged.
	Failed func(err error)
	// WebToggle starts or stops the web server, and WebState says whether it
	// runs and at which address (section 4.7).
	WebToggle func()
	WebState  func() (running bool, url string)
	// WebChanged is called after the port or the password changed, so a server
	// that runs can use the new settings.
	WebChanged func()
}

const colorError = "#D55E00"

// userMessage returns the text to show for an error: the message of an
// application error is written for the user, anything else is shown as it is.
func userMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// formatTestResult is the line shown after Test connection.
func formatTestResult(res analysis.TestResult, err error) (text string, ok bool) {
	switch {
	case err != nil:
		return "Failed: " + userMessage(err), false
	case res.Cancelled:
		return "", true
	default:
		return fmt.Sprintf("Connected. The LLM answered in %.1f s.", res.Elapsed.Seconds()), true
	}
}

// startTest runs a connection test in a goroutine and reports on the UI
// thread. alive tells whether the widgets that report is going to touch still
// exist; it is checked on the UI thread, just before report is called.
func startTest(ctx context.Context, backend PreferencesBackend, cfg store.LLMConfig, alive func() bool, report func(text string, ok bool)) {
	go func() {
		res, err := backend.TestLLM(ctx, cfg)
		text, ok := formatTestResult(res, err)
		mainthread.Start(func() {
			if alive() && text != "" {
				report(text, ok)
			}
		})
	}()
}

// PreferencesDialog is the modal Preferences dialog (section 4.5).
type PreferencesDialog struct {
	dlg     *qt.QDialog
	ctx     context.Context
	cancel  context.CancelFunc
	backend PreferencesBackend
	hooks   PreferencesHooks
	closed  bool

	configs    []store.LLMConfig
	list       *qt.QListWidget
	addBtn     *qt.QPushButton
	editBtn    *qt.QPushButton
	deleteBtn  *qt.QPushButton
	testBtn    *qt.QPushButton
	testResult *qt.QLabel
	banner     *qt.QLabel

	autoCheck *qt.QCheckBox
	delay     *qt.QSpinBox
	fontSize  *qt.QSpinBox
	loading   bool // true while the General widgets are filled from settings

	web webTab

	// confirm asks a yes/no question; execForm runs a form dialog. Tests
	// replace them, because both would otherwise block in a modal loop.
	confirm  func(text string) bool
	execForm func(f *LLMForm)
	// askPassword asks for a new password twice and reports false if the user
	// cancelled.
	askPassword func() (string, string, bool)
}

// NewPreferencesDialog builds the dialog and loads the LLM list and the
// general settings into it.
func NewPreferencesDialog(ctx context.Context, parent *qt.QWidget, backend PreferencesBackend, hooks PreferencesHooks) *PreferencesDialog {
	d := &PreferencesDialog{backend: backend, hooks: hooks}
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.dlg = qt.NewQDialog(parent)
	d.dlg.SetWindowTitle("Preferences")
	d.dlg.SetModal(true)
	d.dlg.Resize(560, 420)
	d.confirm = func(text string) bool {
		return qt.QMessageBox_Question2(d.dlg.QWidget, "Preferences", text, qt.QMessageBox__Yes, qt.QMessageBox__No) == int(qt.QMessageBox__Yes)
	}
	d.execForm = func(f *LLMForm) { f.dlg.Exec() }
	d.askPassword = d.askPasswordTwice

	layout := qt.NewQVBoxLayout2()
	d.dlg.SetLayout(layout.QLayout)
	tabs := qt.NewQTabWidget2()
	tabs.AddTab(d.buildLLMsTab(), "LLMs")
	tabs.AddTab(d.buildGeneralTab(), "General")
	tabs.AddTab(d.buildWebTab(), "Web server")
	layout.AddWidget(tabs.QWidget)

	d.banner = qt.NewQLabel3("")
	d.banner.SetWordWrap(true)
	d.banner.SetStyleSheet("color: " + colorError + ";")
	d.banner.Hide()
	layout.AddWidget(d.banner.QWidget)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddStretch()
	closeBtn := qt.NewQPushButton3("Close")
	closeBtn.SetDefault(true)
	closeBtn.OnClicked(d.dlg.Accept)
	buttons.AddWidget(closeBtn.QWidget)
	layout.AddLayout(buttons.QLayout)

	d.dlg.OnFinished(func(int) {
		d.closed = true
		d.cancel()
	})
	d.reload(0)
	d.loadGeneral()
	d.loadWeb()
	return d
}

// Exec shows the dialog and returns when it is closed.
func (d *PreferencesDialog) Exec() { d.dlg.Exec() }

// Dialog returns the dialog widget.
func (d *PreferencesDialog) Dialog() *qt.QDialog { return d.dlg }

func (d *PreferencesDialog) buildLLMsTab() *qt.QWidget {
	tab := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout2()
	tab.SetLayout(layout.QLayout)

	d.list = qt.NewQListWidget2()
	layout.AddWidget(d.list.QWidget)

	row := qt.NewQHBoxLayout2()
	d.addBtn = qt.NewQPushButton3("Add...")
	d.editBtn = qt.NewQPushButton3("Edit...")
	d.deleteBtn = qt.NewQPushButton3("Delete")
	d.testBtn = qt.NewQPushButton3("Test connection")
	for _, b := range []*qt.QPushButton{d.addBtn, d.editBtn, d.deleteBtn, d.testBtn} {
		row.AddWidget(b.QWidget)
	}
	row.AddStretch()
	layout.AddLayout(row.QLayout)
	d.testResult = qt.NewQLabel3("")
	d.testResult.SetWordWrap(true)
	layout.AddWidget(d.testResult.QWidget)

	d.addBtn.OnClicked(func() { d.editLLM(store.LLMConfig{Temperature: store.DefaultTemperature, NoThinking: true}) })
	d.editBtn.OnClicked(func() {
		if cfg, ok := d.current(); ok {
			d.editLLM(cfg)
		}
	})
	d.deleteBtn.OnClicked(d.deleteCurrent)
	d.testBtn.OnClicked(d.testCurrent)
	d.list.OnItemDoubleClicked(func(*qt.QListWidgetItem) {
		if cfg, ok := d.current(); ok {
			d.editLLM(cfg)
		}
	})
	d.list.OnCurrentRowChanged(func(int) { d.updateButtons() })
	return tab
}

func (d *PreferencesDialog) buildGeneralTab() *qt.QWidget {
	tab := qt.NewQWidget2()
	form := qt.NewQFormLayout2()
	tab.SetLayout(form.QLayout)

	d.autoCheck = qt.NewQCheckBox3("Check automatically when I pause typing")
	d.autoCheck.SetToolTip("Off by default. The Check button always works.")
	form.AddRow3("Auto-check", d.autoCheck.QWidget)
	d.delay = qt.NewQSpinBox2()
	d.delay.SetRange(1, 600)
	d.delay.SetSuffix(" s")
	d.delay.SetToolTip("How long to wait after the last key press. Used only when auto-check is on.")
	form.AddRow3("Auto-check delay", d.delay.QWidget)
	d.fontSize = qt.NewQSpinBox2()
	d.fontSize.SetRange(6, 48)
	d.fontSize.SetSuffix(" pt")
	form.AddRow3("Editor font size", d.fontSize.QWidget)

	d.autoCheck.OnToggled(func(on bool) {
		d.delay.SetEnabled(on)
		d.generalChanged()
	})
	d.delay.OnValueChanged(func(int) { d.generalChanged() })
	d.fontSize.OnValueChanged(func(int) { d.generalChanged() })
	return tab
}

func (d *PreferencesDialog) loadGeneral() {
	g, err := d.backend.General(d.ctx)
	if err != nil {
		d.fail(err)
		g = General{AutoCheckDelayS: store.DefaultAutoCheckSec, FontSize: store.DefaultFontSize}
	}
	d.loading = true
	d.autoCheck.SetChecked(g.AutoCheck)
	d.delay.SetValue(g.AutoCheckDelayS)
	d.delay.SetEnabled(g.AutoCheck)
	d.fontSize.SetValue(g.FontSize)
	d.loading = false
}

func (d *PreferencesDialog) generalChanged() {
	if d.loading {
		return
	}
	err := d.backend.SetGeneral(d.ctx, General{
		AutoCheck:       d.autoCheck.IsChecked(),
		AutoCheckDelayS: d.delay.Value(),
		FontSize:        d.fontSize.Value(),
	})
	if err != nil {
		d.fail(err)
		return
	}
	d.clearBanner()
	call(d.hooks.Changed)
}

// reload refills the list and selects the entry with the given ID (or the
// first one).
func (d *PreferencesDialog) reload(selectID uint) {
	cfgs, err := d.backend.ListLLMs(d.ctx)
	if err != nil {
		d.fail(err)
		cfgs = nil
	}
	d.configs = cfgs
	d.list.Clear()
	row := 0
	for i, c := range cfgs {
		d.list.AddItem(fmt.Sprintf("%s  —  %s · %s", c.Name, c.Provider, c.Model))
		if c.ID == selectID {
			row = i
		}
	}
	if len(cfgs) == 0 {
		d.list.AddItem("No LLMs yet. Press Add to create one.")
		d.list.Item(0).SetFlags(qt.NoItemFlags)
	} else {
		d.list.SetCurrentRow(row)
	}
	d.testResult.SetText("")
	d.updateButtons()
}

func (d *PreferencesDialog) current() (store.LLMConfig, bool) {
	row := d.list.CurrentRow()
	if row < 0 || row >= len(d.configs) {
		return store.LLMConfig{}, false
	}
	return d.configs[row], true
}

func (d *PreferencesDialog) updateButtons() {
	_, ok := d.current()
	d.editBtn.SetEnabled(ok)
	d.deleteBtn.SetEnabled(ok)
	d.testBtn.SetEnabled(ok)
}

func (d *PreferencesDialog) editLLM(cfg store.LLMConfig) {
	form := NewLLMForm(d.ctx, d.dlg.QWidget, d.backend, cfg, func(c *store.LLMConfig) error {
		return d.backend.SaveLLM(d.ctx, c)
	})
	defer form.dlg.Delete()
	d.execForm(form)
	if !form.Saved() {
		return
	}
	d.clearBanner()
	d.reload(form.SavedID())
	call(d.hooks.Changed)
}

func (d *PreferencesDialog) deleteCurrent() {
	cfg, ok := d.current()
	if !ok || !d.confirm(fmt.Sprintf("Delete the LLM %q? This cannot be undone.", cfg.Name)) {
		return
	}
	if err := d.backend.DeleteLLM(d.ctx, cfg.ID); err != nil {
		d.fail(err)
		d.reload(cfg.ID) // the list may be stale; show what is really there
		return
	}
	d.clearBanner()
	d.reload(0)
	call(d.hooks.Changed)
}

func (d *PreferencesDialog) testCurrent() {
	cfg, ok := d.current()
	if !ok {
		return
	}
	d.testResult.SetStyleSheet("")
	d.testResult.SetText("Testing...")
	startTest(d.ctx, d.backend, cfg, func() bool { return !d.closed }, func(text string, ok bool) {
		showResult(d.testResult, text, ok)
	})
}

func showResult(label *qt.QLabel, text string, ok bool) {
	if ok {
		label.SetStyleSheet("")
	} else {
		label.SetStyleSheet("color: " + colorError + ";")
	}
	label.SetText(text)
}

func (d *PreferencesDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner.SetText(userMessage(err))
	d.banner.Show()
}

func (d *PreferencesDialog) clearBanner() {
	d.banner.SetText("")
	d.banner.Hide()
}

// providerHints are the Base URL placeholder and the note shown for a provider.
func providerHints(p string) (placeholder string, note string) {
	switch p {
	case "ollama":
		return "http://localhost:11434", "The address of your Ollama server."
	case "openai-compatible":
		return "https://api.openai.com/v1", "The address of any OpenAI-compatible API, including its /v1 part."
	default:
		return "https://api.anthropic.com (default)", "Optional: leave empty for Anthropic's own server."
	}
}

// defaultBaseURL is filled in when the provider is ollama and the field is empty.
const defaultOllamaURL = "http://localhost:11434"
