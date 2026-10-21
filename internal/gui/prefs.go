package gui

import (
	"context"
	"fmt"

	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// General holds the settings of the General tab.
type General struct {
	AutoCheck       bool
	AutoCheckDelayS int
	FontSize        int
}

// WebSettings holds the settings of the Web server tab (section 4.7). The
// password itself is never shown or kept: only whether there is one.
type WebSettings struct {
	Port        int
	HasPassword bool
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
	// Closed is called once, when the dialog is gone.
	Closed func()
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

// startTest runs a connection test in a goroutine and reports on the UI thread.
// alive tells whether what report is going to touch still exists; it is checked
// on the UI thread, just before report is called.
func startTest(ctx context.Context, rt *Runtime, backend PreferencesBackend, cfg store.LLMConfig, alive func() bool, report func(text string, ok bool)) {
	go func() {
		res, err := backend.TestLLM(ctx, cfg)
		text, ok := formatTestResult(res, err)
		post := func(f func()) { f() }
		if rt != nil {
			post = rt.Post
		}
		post(func() {
			if alive() && text != "" {
				report(text, ok)
			}
		})
	}()
}

const webSecurityNote = "The page is served over plain HTTP, so a password sent from another computer can be read on the network. " +
	"Use it only on a network you trust."

// PreferencesDialog is the modal Preferences dialog (section 4.5).
type PreferencesDialog struct {
	dialogBase
	win     *MainWindow
	ctx     context.Context
	cancel  context.CancelFunc
	backend PreferencesBackend
	hooks   PreferencesHooks
	closed  bool

	tab        int // 0 LLMs, 1 General, 2 Web server
	configs    []store.LLMConfig
	row        int
	testResult string
	testOK     bool
	banner     string

	general General

	web struct {
		settings WebSettings
		running  bool
		url      string
		port     spin
	}

	// confirm asks a yes/no question; showForm shows a form; askPassword asks
	// for a new password twice. Tests replace them.
	confirm     func(text string, done func(yes bool))
	showForm    func(f *LLMForm)
	askPassword func(done func(first, second string, ok bool))
}

// NewPreferencesDialog builds the dialog and loads the LLM list and the
// general settings into it. Show it with win.ShowDialog.
func NewPreferencesDialog(ctx context.Context, win *MainWindow, backend PreferencesBackend, hooks PreferencesHooks) *PreferencesDialog {
	d := &PreferencesDialog{dialogBase: newDialogBase(), win: win, backend: backend, hooks: hooks, row: -1}
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.confirm = func(text string, done func(bool)) { win.Confirm("Preferences", text, done) }
	d.showForm = func(f *LLMForm) { win.ShowDialog(f) }
	d.askPassword = d.askPasswordTwice
	d.reload(0)
	d.loadGeneral()
	d.loadWeb()
	return d
}

// Dismissed releases the dialog.
func (d *PreferencesDialog) Dismissed() {
	d.open, d.closed = false, true
	d.cancel()
	call(d.hooks.Closed)
}

// SelectTab shows the LLMs (0), General (1) or Web server (2) tab.
func (d *PreferencesDialog) SelectTab(i int) { d.tab = min(max(i, 0), 2); d.refresh() }

// ---- LLMs ----

// reload refills the list and selects the entry with the given ID (or the first).
func (d *PreferencesDialog) reload(selectID uint) {
	cfgs, err := d.backend.ListLLMs(d.ctx)
	if err != nil {
		d.fail(err)
		cfgs = nil
	}
	d.configs = cfgs
	d.row = -1
	if len(cfgs) > 0 {
		d.row = 0
	}
	for i, c := range cfgs {
		if c.ID == selectID {
			d.row = i
		}
	}
	d.testResult = ""
	d.refresh()
}

func (d *PreferencesDialog) current() (store.LLMConfig, bool) {
	if d.row < 0 || d.row >= len(d.configs) {
		return store.LLMConfig{}, false
	}
	return d.configs[d.row], true
}

// LLMRows returns the text of each row of the LLM list, or the empty line.
func (d *PreferencesDialog) LLMRows() []string {
	if len(d.configs) == 0 {
		return []string{"No LLMs yet. Press Add to create one."}
	}
	out := make([]string, len(d.configs))
	for i, c := range d.configs {
		out[i] = fmt.Sprintf("%s  —  %s · %s", c.Name, c.Provider, c.Model)
	}
	return out
}

// SelectLLMRow selects a row of the list.
func (d *PreferencesDialog) SelectLLMRow(row int) {
	if row >= 0 && row < len(d.configs) {
		d.row = row
		d.refresh()
	}
}

// SelectedLLMRow returns the selected row, or -1.
func (d *PreferencesDialog) SelectedLLMRow() int { return d.row }

// LLMButtonsEnabled reports whether Edit, Delete and Test connection can be pressed.
func (d *PreferencesDialog) LLMButtonsEnabled() bool { _, ok := d.current(); return ok }

// Add opens the form for a new LLM.
func (d *PreferencesDialog) Add() {
	d.editLLM(store.LLMConfig{Temperature: store.DefaultTemperature, NoThinking: true})
}

// Edit opens the form for the selected LLM.
func (d *PreferencesDialog) Edit() {
	if cfg, ok := d.current(); ok {
		d.editLLM(cfg)
	}
}

func (d *PreferencesDialog) editLLM(cfg store.LLMConfig) {
	form := NewLLMForm(d.ctx, d.win.rt, d.backend, cfg,
		func(c *store.LLMConfig) error { return d.backend.SaveLLM(d.ctx, c) },
		func(saved bool, id uint) {
			if !saved {
				return
			}
			d.clearBanner()
			d.reload(id)
			call(d.hooks.Changed)
		})
	d.showForm(form)
}

// Delete asks, and then deletes the selected LLM.
func (d *PreferencesDialog) Delete() {
	cfg, ok := d.current()
	if !ok {
		return
	}
	d.confirm(fmt.Sprintf("Delete the LLM %q? This cannot be undone.", cfg.Name), func(yes bool) {
		if !yes {
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
	})
}

// Test checks the connection of the selected LLM.
func (d *PreferencesDialog) Test() {
	cfg, ok := d.current()
	if !ok {
		return
	}
	d.testResult, d.testOK = "Testing...", true
	d.refresh()
	startTest(d.ctx, d.win.rt, d.backend, cfg, func() bool { return !d.closed }, func(text string, ok bool) {
		d.testResult, d.testOK = text, ok
		d.refresh()
	})
}

// TestResult returns the line under the list.
func (d *PreferencesDialog) TestResult() string { return d.testResult }

// ---- General ----

func (d *PreferencesDialog) loadGeneral() {
	g, err := d.backend.General(d.ctx)
	if err != nil {
		d.fail(err)
		g = General{AutoCheckDelayS: store.DefaultAutoCheckSec, FontSize: store.DefaultFontSize}
	}
	d.general = g
	d.refresh()
}

// GeneralSettings returns the settings the General tab shows.
func (d *PreferencesDialog) GeneralSettings() General { return d.general }

// SetAutoCheck, SetAutoCheckDelay and SetFontSize change a setting as the user
// would; each is stored at once.
func (d *PreferencesDialog) SetAutoCheck(on bool) { d.general.AutoCheck = on; d.generalChanged() }
func (d *PreferencesDialog) SetAutoCheckDelay(s int) {
	d.general.AutoCheckDelayS = min(max(s, 1), 600)
	d.generalChanged()
}
func (d *PreferencesDialog) SetFontSize(pt int) {
	d.general.FontSize = min(max(pt, 6), 48)
	d.generalChanged()
}

func (d *PreferencesDialog) generalChanged() {
	d.refresh()
	if err := d.backend.SetGeneral(d.ctx, d.general); err != nil {
		d.fail(err)
		return
	}
	d.clearBanner()
	call(d.hooks.Changed)
}

// ---- Web server ----

func (d *PreferencesDialog) loadWeb() {
	s, err := d.backend.WebSettings(d.ctx)
	if err != nil {
		d.fail(err)
		s = WebSettings{Port: store.DefaultWebPort}
	}
	d.web.settings = s
	d.web.port = spin{value: float64(s.Port), min: 1, max: 65535, step: 1}
	d.SetWebState()
}

// SetWebState refreshes the status line and the button from the server. The
// application calls it when the server starts or stops while the dialog is open.
func (d *PreferencesDialog) SetWebState() {
	d.web.running, d.web.url = false, ""
	if d.hooks.WebState != nil {
		d.web.running, d.web.url = d.hooks.WebState()
	}
	d.refresh()
}

// WebStatus returns the status line of the Web server tab.
func (d *PreferencesDialog) WebStatus() string {
	if d.web.running {
		return "The web server is running at " + d.web.url
	}
	return "The web server is stopped."
}

// WebToggleText returns the text of the button that starts or stops the server.
func (d *PreferencesDialog) WebToggleText() string {
	if d.web.running {
		return webStopText
	}
	return webStartText
}

// WebAccessText returns the line about who may open the page.
func (d *PreferencesDialog) WebAccessText() string {
	if d.web.settings.HasPassword {
		return "A password is set. This computer is let in as it is; other computers must sign in with the password."
	}
	return "No password is set, so only this computer can open the page (localhost). " +
		"Set a password to let other computers in."
}

// WebPasswordButtonText returns the text of the button that sets the password.
func (d *PreferencesDialog) WebPasswordButtonText() string {
	if d.web.settings.HasPassword {
		return "Change password..."
	}
	return "Set password..."
}

// WebPortValue returns the port shown.
func (d *PreferencesDialog) WebPortValue() int { return int(d.web.port.value) }

// SetWebPort changes the port as the user would; it is stored at once.
func (d *PreferencesDialog) SetWebPort(port int) {
	port = min(max(port, 1), 65535)
	d.web.port.value = float64(port)
	if err := d.backend.SetWebPort(d.ctx, port); err != nil {
		d.fail(err)
		return
	}
	d.web.settings.Port = port
	d.clearBanner()
	call(d.hooks.WebChanged)
	d.SetWebState()
}

// WebToggleClick presses the button that starts or stops the server.
func (d *PreferencesDialog) WebToggleClick() { call(d.hooks.WebToggle) }

// askPasswordTwice asks for a password and its repetition.
func (d *PreferencesDialog) askPasswordTwice(done func(first, second string, ok bool)) {
	ask := func(label string, then func(string, bool)) {
		t := d.win.AskText("Web server password", label, "", then)
		t.secret = true
	}
	ask("New password", func(first string, ok bool) {
		if !ok {
			done("", "", false)
			return
		}
		ask("Type it again", func(second string, ok bool) { done(first, second, ok) })
	})
}

// SetWebPassword asks for a new password and stores it.
func (d *PreferencesDialog) SetWebPassword() {
	d.askPassword(func(first, second string, ok bool) {
		if !ok {
			return
		}
		if first != second {
			d.banner = "The two passwords are not the same."
			d.refresh()
			return
		}
		if err := d.backend.SetWebPassword(d.ctx, first); err != nil {
			d.fail(err)
			return
		}
		d.web.settings.HasPassword = true
		d.clearBanner()
		call(d.hooks.WebChanged)
		d.SetWebState()
	})
}

// ClearWebPassword asks, and then removes the password.
func (d *PreferencesDialog) ClearWebPassword() {
	if !d.web.settings.HasPassword {
		return
	}
	d.confirm("Remove the password? Then only this computer can open the web page.", func(yes bool) {
		if !yes {
			return
		}
		if err := d.backend.SetWebPassword(d.ctx, ""); err != nil {
			d.fail(err)
			return
		}
		d.web.settings.HasPassword = false
		d.clearBanner()
		call(d.hooks.WebChanged)
		d.SetWebState()
	})
}

// ---- Errors ----

// BannerText returns the message at the bottom of the dialog, or "".
func (d *PreferencesDialog) BannerText() string { return d.banner }

func (d *PreferencesDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner = userMessage(err)
	d.refresh()
}

func (d *PreferencesDialog) clearBanner() { d.banner = ""; d.refresh() }

// ---- Drawing ----

// Build draws the dialog.
func (d *PreferencesDialog) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return d.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		tabs := m.NewRow([]widgets.Widget{
			tabButton("LLMs", d.tab == 0, func() { d.SelectTab(0) }), spaceX(8),
			tabButton("General", d.tab == 1, func() { d.SelectTab(1) }), spaceX(8),
			tabButton("Web server", d.tab == 2, func() { d.SelectTab(2) }),
		})
		content := []func(widgets.BuildContext) widgets.Widget{d.llmsTab, d.generalTab, d.webTab}[d.tab](bc)
		body := []widgets.Widget{tabs, m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(8.0)}), m.NewExpanded(content)}
		if d.banner != "" {
			body = append(body, errorLine(bc, d.banner))
		}
		return frame(bc, "Preferences", 620, 520, m.NewColumn(body, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}),
			primary("Close", d.Close))
	})
}

func (d *PreferencesDialog) llmsTab(bc widgets.BuildContext) widgets.Widget {
	var list widgets.Widget
	if len(d.configs) == 0 {
		list = m.NewText(d.LLMRows()[0])
	} else {
		list = m.NewListViewBuilder(func(bc widgets.BuildContext, i int) widgets.Widget {
			return listRow(bc, d.LLMRows()[i], i == d.row, func() { d.SelectLLMRow(i) }, func() { d.SelectLLMRow(i); d.Edit() })
		}, m.ListViewOpts{ItemCount: foundation.Some(len(d.configs))})
	}
	var edit, del, test func()
	if d.LLMButtonsEnabled() {
		edit, del, test = d.Edit, d.Delete, d.Test
	}
	rows := []widgets.Widget{
		m.NewExpanded(list),
		m.NewWrap([]widgets.Widget{button("Add...", d.Add), button("Edit...", edit), button("Delete", del), button("Test connection", test)},
			m.WrapOpts{Spacing: 8, RunSpacing: 4}),
	}
	if d.testResult != "" {
		if d.testOK {
			rows = append(rows, m.NewSelectionArea(m.NewText(d.testResult)))
		} else {
			rows = append(rows, errorLine(bc, d.testResult))
		}
	}
	return m.NewColumn(rows, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

func (d *PreferencesDialog) generalTab(bc widgets.BuildContext) widgets.Widget {
	g := d.general
	var delay func(float64)
	if g.AutoCheck {
		delay = func(v float64) { d.SetAutoCheckDelay(int(v)) }
	}
	return m.NewColumn([]widgets.Widget{
		tipped("Off by default. The Check button always works.",
			formRow("Auto-check", checkRow("Check automatically when I pause typing", g.AutoCheck, d.SetAutoCheck))),
		tipped("How long to wait after the last key press. Used only when auto-check is on.",
			formRow("Auto-check delay", spin{value: float64(g.AutoCheckDelayS), min: 1, max: 600, step: 1, suffix: " s"}.widget(delay))),
		formRow("Editor font size", spin{value: float64(g.FontSize), min: 6, max: 48, step: 1, suffix: " pt"}.widget(func(v float64) { d.SetFontSize(int(v)) })),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

func (d *PreferencesDialog) webTab(bc widgets.BuildContext) widgets.Widget {
	var clear func()
	if d.web.settings.HasPassword {
		clear = d.ClearWebPassword
	}
	return m.NewSingleChildScrollView(m.NewColumn([]widgets.Widget{
		m.NewText("The web server shows this window in a web browser, with the same functions. Open and Save become Open file and Download."),
		m.NewRow([]widgets.Widget{m.NewExpanded(m.NewSelectionArea(m.NewText(d.WebStatus()))), button(d.WebToggleText(), d.WebToggleClick)}),
		tipped("The default is 3333. A running server restarts on the new port.",
			formRow("Port", d.web.port.widget(func(v float64) { d.SetWebPort(int(v)) }))),
		m.NewText(d.WebAccessText()),
		m.NewWrap([]widgets.Widget{button(d.WebPasswordButtonText(), d.SetWebPassword), button("Remove password", clear)}, m.WrapOpts{Spacing: 8}),
		note(bc, webSecurityNote, true),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}))
}
