package gui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

type fakeBackend struct {
	mu      sync.Mutex
	configs []store.LLMConfig
	nextID  uint
	general General

	saved      []store.LLMConfig
	deleted    []uint
	tested     []store.LLMConfig
	generalSet []General

	web          WebSettings
	webPorts     []int
	webPasswords []string
	webErr       error

	listErr, saveErr, deleteErr, generalErr error
	testResult                              analysis.TestResult
	testErr                                 error
	testGate                                chan struct{} // if set, TestLLM waits for it
}

func newFakeBackend(cfgs ...store.LLMConfig) *fakeBackend {
	b := &fakeBackend{general: General{AutoCheckDelayS: 3, Fonts: store.DefaultFonts()}, web: WebSettings{Port: 3333}, nextID: 100}
	b.configs = cfgs
	return b
}

func (b *fakeBackend) ListLLMs(context.Context) ([]store.LLMConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.configs), b.listErr
}

func (b *fakeBackend) SaveLLM(_ context.Context, cfg *store.LLMConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.saveErr != nil {
		return b.saveErr
	}
	if cfg.ID == 0 {
		b.nextID++
		cfg.ID = b.nextID
		b.configs = append(b.configs, *cfg)
	} else {
		for i := range b.configs {
			if b.configs[i].ID == cfg.ID {
				b.configs[i] = *cfg
			}
		}
	}
	b.saved = append(b.saved, *cfg)
	return nil
}

func (b *fakeBackend) DeleteLLM(_ context.Context, id uint) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.deleted = append(b.deleted, id)
	b.configs = slices.DeleteFunc(b.configs, func(c store.LLMConfig) bool { return c.ID == id })
	return nil
}

func (b *fakeBackend) TestLLM(ctx context.Context, cfg store.LLMConfig) (analysis.TestResult, error) {
	b.mu.Lock()
	b.tested = append(b.tested, cfg)
	gate, res, err := b.testGate, b.testResult, b.testErr
	b.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return analysis.TestResult{Cancelled: true}, nil
		}
	}
	return res, err
}

func (b *fakeBackend) General(context.Context) (General, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.general, b.generalErr
}

func (b *fakeBackend) SetGeneral(_ context.Context, g General) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generalErr != nil {
		return b.generalErr
	}
	b.general = g
	b.generalSet = append(b.generalSet, g)
	return nil
}

func (b *fakeBackend) WebSettings(context.Context) (WebSettings, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.web, b.webErr
}

func (b *fakeBackend) SetWebPort(_ context.Context, port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.webErr != nil {
		return b.webErr
	}
	b.web.Port = port
	b.webPorts = append(b.webPorts, port)
	return nil
}

func (b *fakeBackend) SetWebPassword(_ context.Context, password string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.webErr != nil {
		return b.webErr
	}
	b.web.HasPassword = password != ""
	b.webPasswords = append(b.webPasswords, password)
	return nil
}

// testedConfigs returns the configurations TestLLM was called with. It takes the
// lock because TestLLM runs in a goroutine.
func (b *fakeBackend) testedConfigs() []store.LLMConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.tested)
}

func ollamaCfg(id uint, name string) store.LLMConfig {
	return store.LLMConfig{ID: id, Name: name, Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
}

type hookLog struct {
	changed, webChanged int
	failed              []error
	toggled             int
	running             bool
}

func (h *hookLog) hooks() PreferencesHooks {
	return PreferencesHooks{
		Changed:    func() { h.changed++ },
		Failed:     func(err error) { h.failed = append(h.failed, err) },
		WebToggle:  func() { h.toggled++ },
		WebState:   func() (bool, string) { return h.running, "http://localhost:3333" },
		WebChanged: func() { h.webChanged++ },
	}
}

func showPrefs(t *testing.T, b *fakeBackend) (*gluttertest.Tester, *MainWindow, *PreferencesDialog, *hookLog) {
	t.Helper()
	tester, w := pumpWindow(t)
	h := &hookLog{}
	d := NewPreferencesDialog(context.Background(), w, b, h.hooks())
	d.confirm = func(_ string, done func(bool)) { done(true) }
	w.ShowDialog(d)
	tester.PumpAndSettle()
	return tester, w, d, h
}

// submitForm answers the form of Add or Edit: it runs fill on the form, and
// presses Save when save is true, else Cancel.
func submitForm(tester *gluttertest.Tester, d *PreferencesDialog, fill func(f *LLMForm), save bool) {
	d.showForm = func(f *LLMForm) {
		d.win.ShowDialog(f)
		tester.PumpAndSettle() // the form is on screen before it is filled
		fill(f)
		if save {
			f.Save()
		} else {
			f.Close()
		}
	}
}

// afterForm waits until the dialogs that were closed have reported.
func afterForm(t *testing.T, tester *gluttertest.Tester, cond func() bool) {
	t.Helper()
	waitFor(t, tester, cond)
}

func TestPreferencesShowsTheStoredState(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"), store.LLMConfig{ID: 2, Name: "claude", Provider: "anthropic", Model: "claude-sonnet-5-5"})
	b.general = General{AutoCheck: true, AutoCheckDelayS: 7, Fonts: store.Fonts{
		Editor: store.Font{Size: 15}, Buttons: store.Font{Family: "Gone Sans", Size: 9}, Results: store.Font{Family: "Test Serif", Size: 12},
	}}
	tester, _, d, _ := showPrefs(t, b)
	d.installedFamilies = func(widgets.BuildContext) []string { return []string{"Other", "Test Serif"} }
	if rows := d.LLMRows(); len(rows) != 2 || rows[0] != "local  —  ollama · llama3.1" || d.SelectedLLMRow() != 0 || !d.LLMButtonsEnabled() {
		t.Errorf("rows %q selected %d", rows, d.SelectedLLMRow())
	}
	if g := d.GeneralSettings(); g != b.general {
		t.Errorf("general = %+v", g)
	}
	for _, text := range []string{"Preferences", "LLMs", "General", "Web server", "Add...", "Edit...", "Delete", "Test connection", "Close"} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	d.SelectTab(1)
	tester.Pump()
	for _, text := range []string{"Auto-check", "Auto-check delay", "Editor font", "Buttons font", "Results font", "7 s", "15 pt", "9 pt", "12 pt"} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	// Each list shows the family it has; the others are in the menu it opens.
	for _, text := range []string{defaultFontLabel, "Test Serif", "Gone Sans (not installed)"} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	tests := []struct {
		part FontPart
		want []string
	}{
		{FontEditor, []string{"", "Other", "Test Serif"}},
		{FontButtons, []string{"", "Other", "Test Serif", "Gone Sans"}},
		{FontResults, []string{"", "Other", "Test Serif"}},
	}
	for _, tt := range tests {
		if got := d.FontChoices(tt.part); !slices.Equal(got, tt.want) {
			t.Errorf("%s choices %q, want %q", tt.part.label(), got, tt.want)
		}
	}

	tester.Tap(inDialog("Test Serif"))
	tester.PumpAndSettle()
	tester.Tap(gluttertest.ByText("Other"))
	tester.PumpAndSettle()
	if f := b.general.Fonts; f.Results.Family != "Other" || f.Editor.Family != "" || f.Buttons.Family != "Gone Sans" {
		t.Errorf("after choosing Other for the results: %+v", f)
	}
}

func TestPreferencesDefaultsWithoutLLMs(t *testing.T) {
	_, _, d, _ := showPrefs(t, newFakeBackend())
	if rows := d.LLMRows(); len(rows) != 1 || !strings.Contains(rows[0], "No LLMs yet") || d.LLMButtonsEnabled() {
		t.Errorf("rows %q enabled %v", rows, d.LLMButtonsEnabled())
	}
	d.Edit()
	d.Delete()
	d.Test()
}

func TestAddLLM(t *testing.T) {
	b := newFakeBackend()
	tester, _, d, h := showPrefs(t, b)
	submitForm(tester, d, func(f *LLMForm) {
		f.name.SetText("  My Claude ")
		f.SetProvider("anthropic")
		f.model.SetText("claude-sonnet-5-5")
		f.apiKey.SetText("sk-secret")
		f.baseURL.SetText("https://llm.example/v1")
		f.SetTemperature(0.5)
		f.SetParallel(3)
	}, true)
	d.Add()
	afterForm(t, tester, func() bool { return h.changed == 1 })
	if len(b.saved) != 1 {
		t.Fatalf("saved %d", len(b.saved))
	}
	got := b.saved[0]
	if got.Name != "My Claude" || got.Provider != "anthropic" || got.Model != "claude-sonnet-5-5" || got.APIKey != "sk-secret" ||
		got.Temperature != 0.5 || got.MaxParallel != 3 || got.ID == 0 {
		t.Errorf("saved %+v", got)
	}
	if h.changed != 1 || len(d.LLMRows()) != 1 || d.SelectedLLMRow() != 0 {
		t.Errorf("changed %d rows %q selected %d", h.changed, d.LLMRows(), d.SelectedLLMRow())
	}
}

func TestAddFormStartsWithOpenAICompatibleDefaults(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	var form *LLMForm
	submitForm(tester, d, func(f *LLMForm) { form = f }, false)
	d.Add()
	if form.ProviderName() != "openai-compatible" || !form.NoThinkingEnabled() || !form.Config().NoThinking || form.Config().Temperature != store.DefaultTemperature {
		t.Errorf("provider %q config %+v", form.ProviderName(), form.Config())
	}
}

func TestEditLLMAndCancel(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"), ollamaCfg(2, "other"))
	tester, _, d, h := showPrefs(t, b)
	d.SelectLLMRow(1)
	submitForm(tester, d, func(f *LLMForm) {
		if f.name.Text() != "other" || f.model.Text() != "llama3.1" {
			t.Errorf("the form starts with %q / %q", f.name.Text(), f.model.Text())
		}
		f.name.SetText("renamed")
	}, true)
	d.Edit()
	afterForm(t, tester, func() bool { return h.changed == 1 })
	if len(b.saved) != 1 || b.saved[0].ID != 2 || b.saved[0].Name != "renamed" || d.SelectedLLMRow() != 1 || h.changed != 1 {
		t.Errorf("saved %+v selected %d changed %d", b.saved, d.SelectedLLMRow(), h.changed)
	}
	submitForm(tester, d, func(f *LLMForm) { f.name.SetText("never saved") }, false)
	d.Edit()
	tester.PumpAndSettle()
	time.Sleep(30 * time.Millisecond)
	tester.FlushPosted()
	if len(b.saved) != 1 || h.changed != 1 {
		t.Errorf("a cancelled form changed something: saved %d changed %d", len(b.saved), h.changed)
	}
}

func TestFormValidationAndBackendErrorsKeepTheFormOpen(t *testing.T) {
	b := newFakeBackend()
	tester, w, d, _ := showPrefs(t, b)
	var form *LLMForm
	d.showForm = func(f *LLMForm) { form = f; w.ShowDialog(f) }
	d.Add()
	tester.PumpAndSettle()
	form.Save()
	if form.Message() == "" || form.Saved() || !form.IsOpen() || len(b.saved) != 0 {
		t.Errorf("an empty form: message %q saved %v", form.Message(), form.Saved())
	}
	form.name.SetText("x")
	form.model.SetText("m")
	form.baseURL.SetText("https://llm.example/v1")
	b.saveErr = errors.New("LLMConfigNameTaken", "That name is already used.")
	form.Save()
	if form.Message() != "That name is already used." || form.Saved() || !form.IsOpen() {
		t.Errorf("message %q saved %v open %v", form.Message(), form.Saved(), form.IsOpen())
	}
	b.saveErr = nil
	form.Save()
	tester.PumpAndSettle()
	if !form.Saved() || form.SavedID() == 0 || len(b.saved) != 1 {
		t.Errorf("saved %v id %d", form.Saved(), form.SavedID())
	}
}

func TestProviderSwitching(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	var form *LLMForm
	submitForm(tester, d, func(f *LLMForm) { form = f }, false)
	d.Add()
	form.SetProvider("ollama")
	if form.baseURL.Text() != defaultOllamaURL || form.KeyEnabled() || form.NoThinkingEnabled() == false {
		t.Errorf("ollama: url %q key %v", form.baseURL.Text(), form.KeyEnabled())
	}
	form.apiKey.SetText("leftover")
	if form.Config().APIKey != "" {
		t.Error("an Ollama configuration must not keep an API key")
	}
	form.SetProvider("anthropic")
	if form.baseURL.Text() != "" || !form.KeyEnabled() || form.NoThinkingEnabled() {
		t.Errorf("anthropic: url %q key %v nothinking %v", form.baseURL.Text(), form.KeyEnabled(), form.NoThinkingEnabled())
	}
	if form.Config().NoThinking {
		t.Error("No thinking does nothing for Anthropic and is not stored")
	}
	form.baseURL.SetText("https://custom.example/v1")
	form.SetProvider("openai-compatible")
	if form.baseURL.Text() != "https://custom.example/v1" {
		t.Errorf("a typed URL is kept, got %q", form.baseURL.Text())
	}
	form.SetProvider("nonsense")
	if form.ProviderName() != "openai-compatible" {
		t.Errorf("an unknown provider changed the choice to %q", form.ProviderName())
	}
}

func TestApiKeyIsMaskedUntilShown(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	var form *LLMForm
	submitForm(tester, d, func(f *LLMForm) { form = f }, false)
	d.Add()
	if form.KeyShown() {
		t.Error("the key is masked when the form opens")
	}
	form.SetKeyShown(true)
	if !form.KeyShown() {
		t.Error("Show should reveal the key")
	}
	form.SetKeyShown(false)
	if form.KeyShown() {
		t.Error("Hide should mask it again")
	}
}

func TestFormShowsThePrivacyNoteForRemoteServers(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	var form *LLMForm
	submitForm(tester, d, func(f *LLMForm) { form = f }, false)
	d.Add()
	form.SetProvider("ollama")
	if form.PrivacyNoteText() != "" {
		t.Errorf("a local server needs no note, got %q", form.PrivacyNoteText())
	}
	form.baseURL.SetText("https://llm.example.com")
	if !strings.Contains(form.PrivacyNoteText(), "llm.example.com") {
		t.Errorf("note = %q", form.PrivacyNoteText())
	}
	form.SetProvider("anthropic")
	form.baseURL.SetText("")
	if !strings.Contains(form.PrivacyNoteText(), "Anthropic") {
		t.Errorf("note = %q", form.PrivacyNoteText())
	}
}

func TestNumberFieldsStayInRange(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	var form *LLMForm
	submitForm(tester, d, func(f *LLMForm) { form = f }, false)
	d.Add()
	form.SetTemperature(5)
	form.SetTimeout(-4)
	form.SetParallel(0)
	c := form.Config()
	if c.Temperature != 2 || c.TimeoutSeconds != 0 || c.MaxParallel != 1 {
		t.Errorf("config %+v", c)
	}
	if got := form.timeout.text(); got != "Default" {
		t.Errorf("zero timeout shows %q", got)
	}
	form.SetTimeout(90)
	if got := form.timeout.text(); got != "90 s" {
		t.Errorf("timeout shows %q", got)
	}
	if s := (spin{value: 0.3, min: 0, max: 2, step: 0.1, decimals: 2}); s.next(1) != 0.4 || s.next(-10) != 0 {
		t.Errorf("0.3 + a step = %v", s.next(1))
	}
}

func TestDeleteLLM(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "a"), ollamaCfg(2, "b"))
	_, _, d, h := showPrefs(t, b)
	d.SelectLLMRow(1)
	var asked string
	d.confirm = func(text string, done func(bool)) { asked = text; done(false) }
	d.Delete()
	if len(b.deleted) != 0 || !strings.Contains(asked, `"b"`) {
		t.Errorf("declined: deleted %v asked %q", b.deleted, asked)
	}
	d.confirm = func(_ string, done func(bool)) { done(true) }
	d.Delete()
	if !slices.Equal(b.deleted, []uint{2}) || len(d.LLMRows()) != 1 || h.changed != 1 {
		t.Errorf("deleted %v rows %q changed %d", b.deleted, d.LLMRows(), h.changed)
	}
	b.deleteErr = errors.New("LLMConfigNotFound", "That LLM no longer exists.")
	d.Delete()
	if d.BannerText() != "That LLM no longer exists." || len(h.failed) != 1 {
		t.Errorf("banner %q failed %v", d.BannerText(), h.failed)
	}
}

func TestLoadFailureShowsABannerAndDefaults(t *testing.T) {
	b := newFakeBackend()
	b.listErr = errors.New("LLMConfigListFailed", "The list could not be read.")
	b.generalErr = errors.New("SettingReadFailed", "A setting could not be read.")
	b.webErr = errors.New("SettingReadFailed", "A web setting could not be read.")
	_, _, d, h := showPrefs(t, b)
	if d.BannerText() == "" || len(h.failed) != 3 {
		t.Errorf("banner %q failed %d", d.BannerText(), len(h.failed))
	}
	if g := d.GeneralSettings(); g.AutoCheckDelayS != store.DefaultAutoCheckSec || g.Fonts != store.DefaultFonts() || d.WebPortValue() != store.DefaultWebPort {
		t.Errorf("defaults: %+v port %d", g, d.WebPortValue())
	}
}

func TestGeneralSettingsAreStoredAtOnce(t *testing.T) {
	b := newFakeBackend()
	tester, _, d, h := showPrefs(t, b)
	d.SelectTab(1)
	tester.Pump()
	tester.Tap(gluttertest.Descendant(gluttertest.ByType[*m.Dialog](), gluttertest.ByType[*m.Checkbox]()))
	tester.Pump()
	if !b.general.AutoCheck || h.changed != 1 {
		t.Errorf("general %+v changed %d", b.general, h.changed)
	}
	d.SetAutoCheckDelay(9)
	d.SetFontSize(FontEditor, 100) // out of range: held at the maximum
	d.SetFontSize(FontButtons, 2)  // and at the minimum
	d.SetFontFamily(FontResults, "Test Serif")
	want := store.Fonts{Editor: store.Font{Size: 48}, Buttons: store.Font{Size: 6}, Results: store.Font{Family: "Test Serif", Size: store.DefaultResultsFontSize}}
	if g := b.general; g.AutoCheckDelayS != 9 || g.Fonts != want || h.changed != 5 {
		t.Errorf("general %+v changed %d", g, h.changed)
	}
	b.generalErr = errors.New("SettingSaveFailed", "The setting could not be saved.")
	d.SetFontSize(FontEditor, 12)
	if d.BannerText() != "The setting could not be saved." || len(h.failed) != 1 || h.changed != 5 {
		t.Errorf("banner %q failed %v", d.BannerText(), h.failed)
	}
}

func TestTestConnectionFromTheList(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	b.testResult = analysis.TestResult{Elapsed: 1500 * time.Millisecond}
	tester, _, d, _ := showPrefs(t, b)
	tapInDialog(t, tester, "Test connection")
	waitFor(t, tester, func() bool { return strings.Contains(d.TestResult(), "Connected. The LLM answered in 1.5 s.") })
	if got := b.testedConfigs(); len(got) != 1 || got[0].Name != "local" {
		t.Errorf("tested %+v", got)
	}

	b.mu.Lock()
	b.testErr = errors.New("LLMCallFailed", "The request failed: refused")
	b.mu.Unlock()
	d.Test()
	waitFor(t, tester, func() bool { return strings.Contains(d.TestResult(), "Failed: The request failed: refused") })
}

func TestClosingTheDialogDropsALateTestResult(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	b.testGate = make(chan struct{})
	tester, _, d, _ := showPrefs(t, b)
	d.Test()
	if d.TestResult() != "Testing..." {
		t.Fatalf("result = %q", d.TestResult())
	}
	d.Close()
	tester.PumpAndSettle()
	close(b.testGate)
	time.Sleep(50 * time.Millisecond)
	tester.Pump()
	if d.TestResult() != "Testing..." {
		t.Errorf("a result after closing was shown: %q", d.TestResult())
	}
}

func TestFormTestConnection(t *testing.T) {
	b := newFakeBackend()
	b.testResult = analysis.TestResult{Elapsed: 2 * time.Second}
	tester, _, d, _ := showPrefs(t, b)
	var form *LLMForm
	d.showForm = func(f *LLMForm) { form = f; d.win.ShowDialog(f); tester.PumpAndSettle() }
	d.Add()
	form.Test()
	if form.Message() == "" || len(b.testedConfigs()) != 0 {
		t.Errorf("an invalid form is not tested: message %q", form.Message())
	}
	form.model.SetText("m")
	form.baseURL.SetText("https://llm.example/v1")
	form.Test() // no name is needed to test
	waitFor(t, tester, func() bool { return strings.Contains(form.TestResult(), "Connected") })
	if got := b.testedConfigs(); len(got) != 1 || got[0].Name != "this LLM" {
		t.Errorf("tested %+v", got)
	}
}

func TestFormatTestResult(t *testing.T) {
	tests := []struct {
		name string
		res  analysis.TestResult
		err  error
		text string
		ok   bool
	}{
		{"connected", analysis.TestResult{Elapsed: 2300 * time.Millisecond}, nil, "Connected. The LLM answered in 2.3 s.", true},
		{"cancelled says nothing", analysis.TestResult{Cancelled: true}, nil, "", true},
		{"failed", analysis.TestResult{}, errors.New("LLMCallFailed", "The request failed."), "Failed: The request failed.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok := formatTestResult(tt.res, tt.err)
			if text != tt.text || ok != tt.ok {
				t.Errorf("= %q, %v", text, ok)
			}
		})
	}
}

// ---- Web server tab ----

func TestWebTabShowsTheStoredSettings(t *testing.T) {
	b := newFakeBackend()
	b.web = WebSettings{Port: 4444, HasPassword: true}
	tester, _, d, h := showPrefs(t, b)
	d.SelectTab(2)
	tester.Pump()
	if d.WebPortValue() != 4444 || !strings.Contains(d.WebAccessText(), "A password is set") || d.WebPasswordButtonText() != "Change password..." {
		t.Errorf("port %d access %q button %q", d.WebPortValue(), d.WebAccessText(), d.WebPasswordButtonText())
	}
	if d.WebStatus() != "The web server is stopped." || d.WebToggleText() != webStartText {
		t.Errorf("status %q button %q", d.WebStatus(), d.WebToggleText())
	}
	h.running = true
	d.SetWebState()
	tester.Pump()
	if d.WebStatus() != "The web server is running at http://localhost:3333" || d.WebToggleText() != webStopText {
		t.Errorf("status %q button %q", d.WebStatus(), d.WebToggleText())
	}
	tapInDialog(t, tester, webStopText)
	if h.toggled != 1 {
		t.Errorf("toggled %d", h.toggled)
	}
}

func TestWebPortChangeIsStoredAndReported(t *testing.T) {
	b := newFakeBackend()
	_, _, d, h := showPrefs(t, b)
	d.SetWebPort(4000)
	d.SetWebPort(70000) // held at the largest port
	if !slices.Equal(b.webPorts, []int{4000, 65535}) || h.webChanged != 2 || d.WebPortValue() != 65535 {
		t.Errorf("ports %v changed %d shown %d", b.webPorts, h.webChanged, d.WebPortValue())
	}
	b.webErr = errors.New("SettingSaveFailed", "The port could not be saved.")
	d.SetWebPort(5000)
	if d.BannerText() != "The port could not be saved." || len(b.webPorts) != 2 {
		t.Errorf("banner %q ports %v", d.BannerText(), b.webPorts)
	}
}

func TestWebPasswordSetAndRemove(t *testing.T) {
	b := newFakeBackend()
	_, _, d, h := showPrefs(t, b)
	d.askPassword = func(done func(string, string, bool)) { done("one", "two", true) }
	d.SetWebPassword()
	if len(b.webPasswords) != 0 || d.BannerText() != "The two passwords are not the same." {
		t.Errorf("mismatch stored %v banner %q", b.webPasswords, d.BannerText())
	}
	d.askPassword = func(done func(string, string, bool)) { done("", "", false) }
	d.SetWebPassword()
	if len(b.webPasswords) != 0 {
		t.Error("cancel stores nothing")
	}
	d.askPassword = func(done func(string, string, bool)) { done("secret", "secret", true) }
	d.SetWebPassword()
	if !slices.Equal(b.webPasswords, []string{"secret"}) || d.WebPasswordButtonText() != "Change password..." || h.webChanged != 1 || d.BannerText() != "" {
		t.Errorf("stored %v button %q changed %d banner %q", b.webPasswords, d.WebPasswordButtonText(), h.webChanged, d.BannerText())
	}
	d.confirm = func(_ string, done func(bool)) { done(false) }
	d.ClearWebPassword()
	if len(b.webPasswords) != 1 {
		t.Error("declining keeps the password")
	}
	d.confirm = func(_ string, done func(bool)) { done(true) }
	d.ClearWebPassword()
	if len(b.webPasswords) != 2 || b.webPasswords[1] != "" || d.WebPasswordButtonText() != "Set password..." || h.webChanged != 2 {
		t.Errorf("stored %v button %q", b.webPasswords, d.WebPasswordButtonText())
	}
}

func TestAskingForThePasswordTwiceUsesTwoQuestions(t *testing.T) {
	tester, _, d, _ := showPrefs(t, newFakeBackend())
	d.SelectTab(2)
	tester.Pump()
	var first, second string
	var ok bool
	d.askPassword = d.askPasswordTwice
	d.askPasswordTwice(func(a, b string, k bool) { first, second, ok = a, b, k })
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByType[*m.TextField]()); err != nil {
		t.Fatal(err)
	}
	tester.EnterText(gluttertest.ByType[*m.TextField](), "pw")
	tapInDialog(t, tester, "OK")
	waitFor(t, tester, func() bool {
		return gluttertest.FindsOneWidget(tester, gluttertest.ByText("Type it again")) == nil
	})
	tester.PumpAndSettle()
	tester.EnterText(gluttertest.ByType[*m.TextField](), "pw")
	tapInDialog(t, tester, "OK")
	waitFor(t, tester, func() bool { return ok })
	if first != "pw" || second != "pw" || !ok {
		t.Errorf("got %q %q %v", first, second, ok)
	}
}

// waitFor pumps the app until cond holds, giving a goroutine up to five seconds to post.
func waitFor(t *testing.T, tester *gluttertest.Tester, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tester.FlushPosted()
		tester.Pump()
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("the condition did not hold in time")
}
