package ui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/uitest"
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
	b := &fakeBackend{general: General{AutoCheckDelayS: 3, FontSize: 13}, web: WebSettings{Port: 3333}, nextID: 100}
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
	changed int
	failed  []error
}

func (h *hookLog) hooks() PreferencesHooks {
	return PreferencesHooks{Changed: func() { h.changed++ }, Failed: func(err error) { h.failed = append(h.failed, err) }}
}

func withPrefs(t *testing.T, b *fakeBackend, f func(d *PreferencesDialog, h *hookLog)) {
	t.Helper()
	uitest.OnUI(func() {
		h := &hookLog{}
		d := NewPreferencesDialog(context.Background(), nil, b, h.hooks())
		defer d.dlg.Delete()
		d.confirm = func(string) bool { return true }
		f(d, h)
	})
}

// waitLabel polls a label (from outside the UI thread) until it contains want.
func waitLabel(t *testing.T, get func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		uitest.OnUI(func() { got = get() })
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("label = %q, want it to contain %q", got, want)
}

func TestPreferencesShowsTheStoredState(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"), ollamaCfg(2, "other"))
	b.general = General{AutoCheck: true, AutoCheckDelayS: 7, FontSize: 16}
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		if d.list.Count() != 2 || !strings.HasPrefix(d.list.Item(0).Text(), "local") || !strings.Contains(d.list.Item(1).Text(), "ollama · llama3.1") {
			t.Errorf("list rows are wrong: %d rows, first %q", d.list.Count(), d.list.Item(0).Text())
		}
		if d.list.CurrentRow() != 0 || !d.editBtn.IsEnabled() || !d.deleteBtn.IsEnabled() || !d.testBtn.IsEnabled() {
			t.Error("the first row should be selected and the buttons enabled")
		}
		if !d.autoCheck.IsChecked() || d.delay.Value() != 7 || d.fontSize.Value() != 16 || !d.delay.IsEnabled() {
			t.Errorf("general tab = %v %d %d", d.autoCheck.IsChecked(), d.delay.Value(), d.fontSize.Value())
		}
		if h.changed != 0 || len(b.generalSet) != 0 {
			t.Error("filling the dialog must not count as a change")
		}
	})
}

func TestPreferencesDefaults(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		if d.list.Count() != 1 || d.list.Item(0).Text() != "No LLMs yet. Press Add to create one." {
			t.Error("an empty list shows a hint line")
		}
		if d.editBtn.IsEnabled() || d.deleteBtn.IsEnabled() || d.testBtn.IsEnabled() {
			t.Error("with no LLM the edit, delete and test buttons are disabled")
		}
		if !d.addBtn.IsEnabled() {
			t.Error("Add must stay enabled")
		}
		if d.autoCheck.IsChecked() || d.delay.IsEnabled() {
			t.Error("auto-check is off by default and its delay is disabled then")
		}
		if d.delay.Value() != 3 {
			t.Errorf("delay = %d, want 3", d.delay.Value())
		}
	})
}

func TestAddLLM(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.execForm = func(f *LLMForm) {
			if f.dlg.WindowTitle() != "Add LLM" {
				t.Errorf("title = %q", f.dlg.WindowTitle())
			}
			f.name.SetText("  Claude  ")
			f.provider.SetCurrentIndex(indexOfProvider("anthropic"))
			f.model.SetText("claude-sonnet-5-5")
			f.apiKey.SetText("sk-secret")
			f.temperature.SetValue(0.5)
			f.timeout.SetValue(240)
			f.parallel.SetValue(4)
			f.saveBtn.Click()
		}
		d.addBtn.Click()
		if len(b.saved) != 1 {
			t.Fatalf("saved = %+v", b.saved)
		}
		got := b.saved[0]
		if got.ID == 0 || got.Name != "Claude" || got.Provider != "anthropic" || got.Model != "claude-sonnet-5-5" ||
			got.APIKey != "sk-secret" || got.BaseURL != "" || got.Temperature != 0.5 || got.TimeoutSeconds != 240 || got.MaxParallel != 4 {
			t.Errorf("saved config = %+v", got)
		}
		if h.changed != 1 || d.list.Count() != 2 {
			t.Errorf("changed=%d rows=%d", h.changed, d.list.Count())
		}
		if cur, ok := d.current(); !ok || cur.Name != "Claude" {
			t.Errorf("the new LLM should be selected, got %+v", cur)
		}
	})
}

func TestAddFormStartsWithOpenAICompatibleDefaults(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		var seen store.LLMConfig
		d.execForm = func(f *LLMForm) {
			seen = f.Config()
			if !f.apiKey.IsEnabled() {
				t.Error("an OpenAI-compatible server may need an API key")
			}
			// Choosing ollama still fills in its address.
			f.provider.SetCurrentIndex(indexOfProvider("ollama"))
			if got := f.baseURL.Text(); got != "http://localhost:11434" {
				t.Errorf("ollama address = %q", got)
			}
		}
		d.addBtn.Click()
		if seen.Provider != "openai-compatible" || seen.BaseURL != "" || seen.Temperature != 0.2 || seen.TimeoutSeconds != 0 || seen.MaxParallel != 1 {
			t.Errorf("new form = %+v", seen)
		}
	})
}

func TestEditLLM(t *testing.T) {
	cfg := ollamaCfg(5, "local")
	b := newFakeBackend(cfg)
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.execForm = func(f *LLMForm) {
			if f.dlg.WindowTitle() != "Edit LLM" || f.name.Text() != "local" || f.model.Text() != "llama3.1" || f.baseURL.Text() != "http://localhost:11434" {
				t.Errorf("form not filled: %q %q %q", f.name.Text(), f.model.Text(), f.baseURL.Text())
			}
			f.name.SetText("renamed")
			f.saveBtn.Click()
		}
		d.editBtn.Click()
		if len(b.saved) != 1 || b.saved[0].ID != 5 || b.saved[0].Name != "renamed" {
			t.Errorf("saved = %+v", b.saved)
		}
		if h.changed != 1 || !strings.HasPrefix(d.list.Item(0).Text(), "renamed") {
			t.Errorf("changed=%d first row=%q", h.changed, d.list.Item(0).Text())
		}
	})
}

func TestEditByDoubleClick(t *testing.T) {
	withPrefs(t, newFakeBackend(ollamaCfg(1, "local")), func(d *PreferencesDialog, _ *hookLog) {
		opened := 0
		d.execForm = func(*LLMForm) { opened++ }
		d.list.ItemDoubleClicked(d.list.Item(0))
		if opened != 1 {
			t.Errorf("form opened %d times", opened)
		}
	})
}

func TestCancelledFormChangesNothing(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.execForm = func(f *LLMForm) {
			f.name.SetText("changed but cancelled")
			f.dlg.Reject()
		}
		d.editBtn.Click()
		if len(b.saved) != 0 || h.changed != 0 || !strings.HasPrefix(d.list.Item(0).Text(), "local") {
			t.Error("cancel must not save or notify")
		}
	})
}

func TestFormValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(f *LLMForm)
		wantMsg string
	}{
		{"no name", func(f *LLMForm) { f.name.SetText("") }, "Name is required"},
		{"no model", func(f *LLMForm) { f.model.SetText("") }, "Model is required"},
		{"no base url", func(f *LLMForm) { f.baseURL.SetText("") }, "Base URL"},
		{"bad base url", func(f *LLMForm) { f.baseURL.SetText("localhost:11434") }, "Base URL must be"},
		{"anthropic without key", func(f *LLMForm) {
			f.provider.SetCurrentIndex(indexOfProvider("anthropic"))
			f.apiKey.SetText("")
		}, "API key is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newFakeBackend()
			withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
				d.execForm = func(f *LLMForm) {
					f.name.SetText("x")
					f.model.SetText("m")
					tt.mutate(f)
					f.saveBtn.Click()
					if f.Saved() || f.message.IsHidden() || !strings.Contains(f.message.Text(), tt.wantMsg) {
						t.Errorf("saved=%v message=%q hidden=%v, want a message containing %q", f.Saved(), f.message.Text(), f.message.IsHidden(), tt.wantMsg)
					}
					f.dlg.Reject()
				}
				d.addBtn.Click()
				if len(b.saved) != 0 || h.changed != 0 {
					t.Error("an invalid form must not reach the backend")
				}
			})
		})
	}
}

func TestFormShowsBackendErrorsAndStaysOpen(t *testing.T) {
	b := newFakeBackend()
	b.saveErr = errors.New("LLMConfigNameTaken", "An LLM with this name already exists.")
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.execForm = func(f *LLMForm) {
			f.provider.SetCurrentIndex(indexOfProvider("ollama")) // fills in the address
			f.name.SetText("dup")
			f.model.SetText("m")
			f.saveBtn.Click()
			if f.Saved() || !strings.Contains(f.message.Text(), "already exists") || f.message.IsHidden() {
				t.Errorf("saved=%v message=%q", f.Saved(), f.message.Text())
			}
			f.dlg.Reject()
		}
		d.addBtn.Click()
		if h.changed != 0 {
			t.Error("a failed save is not a change")
		}
	})
}

func TestProviderSwitching(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		d.execForm = func(f *LLMForm) {
			f.name.SetText("x")
			f.model.SetText("m")
			f.apiKey.SetText("sk-typed-before-switching")

			f.provider.SetCurrentIndex(indexOfProvider("openai-compatible"))
			if !f.apiKey.IsEnabled() || !strings.Contains(f.baseURL.PlaceholderText(), "api.openai.com") {
				t.Errorf("openai-compatible: key enabled=%v placeholder=%q", f.apiKey.IsEnabled(), f.baseURL.PlaceholderText())
			}
			f.provider.SetCurrentIndex(indexOfProvider("anthropic"))
			if !strings.Contains(f.baseURL.PlaceholderText(), "anthropic") || !strings.Contains(f.providerNote.Text(), "Optional") {
				t.Errorf("anthropic: placeholder=%q note=%q", f.baseURL.PlaceholderText(), f.providerNote.Text())
			}
			f.provider.SetCurrentIndex(indexOfProvider("ollama"))
			if f.apiKey.IsEnabled() {
				t.Error("ollama does not use a key")
			}
			if got := f.Config().APIKey; got != "" {
				t.Errorf("an unused key must not be stored for ollama, got %q", got)
			}
			f.dlg.Reject()
		}
		d.addBtn.Click()
	})
}

func TestApiKeyFieldIsMaskedAndExplained(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		d.execForm = func(f *LLMForm) {
			if f.apiKey.EchoMode() != qt.QLineEdit__Password {
				t.Error("the API key must be entered in a password field")
			}
			f.dlg.Reject()
		}
		d.addBtn.Click()
	})
	if !strings.Contains(apiKeyNote, "plain text") || !strings.Contains(apiKeyNote, "0600") {
		t.Errorf("the note must say how the key is stored: %q", apiKeyNote)
	}
}

func TestShowAndHideTheApiKey(t *testing.T) {
	cfg := ollamaCfg(1, "cloud")
	cfg.Provider, cfg.APIKey = "anthropic", "sk-secret"
	withPrefs(t, newFakeBackend(cfg), func(d *PreferencesDialog, _ *hookLog) {
		d.execForm = func(f *LLMForm) {
			if f.apiKey.EchoMode() != qt.QLineEdit__Password || f.showKey.Text() != "Show" || !f.showKey.IsEnabled() {
				t.Error("the key starts masked, with a Show button")
			}
			f.showKey.Click()
			if f.apiKey.EchoMode() != qt.QLineEdit__Normal || f.showKey.Text() != "Hide" || f.apiKey.Text() != "sk-secret" {
				t.Errorf("after Show: echo %v, button %q, text %q", f.apiKey.EchoMode(), f.showKey.Text(), f.apiKey.Text())
			}
			f.showKey.Click()
			if f.apiKey.EchoMode() != qt.QLineEdit__Password || f.showKey.Text() != "Show" {
				t.Error("Hide masks the key again")
			}
			f.provider.SetCurrentText("ollama")
			if f.showKey.IsEnabled() {
				t.Error("Ollama has no key, so the button is disabled with the field")
			}
			f.dlg.Reject()
		}
		d.editBtn.Click()
	})
}

func TestDeleteLLM(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"), ollamaCfg(2, "other"))
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		asked := ""
		d.confirm = func(text string) bool { asked = text; return false }
		d.deleteBtn.Click()
		if !strings.Contains(asked, `"local"`) || len(b.deleted) != 0 || h.changed != 0 {
			t.Errorf("declined: asked=%q deleted=%v changed=%d", asked, b.deleted, h.changed)
		}
		d.confirm = func(string) bool { return true }
		d.deleteBtn.Click()
		if !slices.Equal(b.deleted, []uint{1}) || h.changed != 1 || d.list.Count() != 1 {
			t.Errorf("deleted=%v changed=%d rows=%d", b.deleted, h.changed, d.list.Count())
		}
		if cur, ok := d.current(); !ok || cur.ID != 2 {
			t.Error("the remaining LLM should be selected")
		}
		d.deleteBtn.Click()
		if d.editBtn.IsEnabled() || d.list.Item(0).Text() != "No LLMs yet. Press Add to create one." {
			t.Error("after deleting the last LLM the list shows its hint")
		}
	})
}

func TestDeleteFailureShowsABanner(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	b.deleteErr = errors.New("LLMConfigNotFound", "This LLM no longer exists.")
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.deleteBtn.Click()
		if d.banner.IsHidden() || !strings.Contains(d.banner.Text(), "no longer exists") || len(h.failed) != 1 || h.changed != 0 {
			t.Errorf("banner=%q hidden=%v failed=%d changed=%d", d.banner.Text(), d.banner.IsHidden(), len(h.failed), h.changed)
		}
	})
}

func TestLoadFailureShowsABannerAndDefaults(t *testing.T) {
	b := newFakeBackend()
	b.listErr = errors.New("DatabaseQueryFailed", "database is locked")
	b.generalErr = errors.New("DatabaseQueryFailed", "database is locked")
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		if d.banner.IsHidden() || len(h.failed) != 2 {
			t.Errorf("banner hidden=%v failed=%d", d.banner.IsHidden(), len(h.failed))
		}
		if d.autoCheck.IsChecked() || d.delay.Value() != 3 || d.fontSize.Value() != 13 {
			t.Error("a failed read must show the defaults")
		}
	})
}

func TestGeneralSettings(t *testing.T) {
	b := newFakeBackend()
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		d.autoCheck.Click()
		if !d.delay.IsEnabled() {
			t.Error("the delay is enabled while auto-check is on")
		}
		d.delay.SetValue(9)
		d.fontSize.SetValue(20)
		want := []General{
			{AutoCheck: true, AutoCheckDelayS: 3, FontSize: 13},
			{AutoCheck: true, AutoCheckDelayS: 9, FontSize: 13},
			{AutoCheck: true, AutoCheckDelayS: 9, FontSize: 20},
		}
		if !slices.Equal(b.generalSet, want) {
			t.Errorf("settings written = %+v, want %+v", b.generalSet, want)
		}
		if h.changed != 3 {
			t.Errorf("changed = %d, want 3", h.changed)
		}
		d.autoCheck.Click()
		if d.delay.IsEnabled() || b.general.AutoCheck {
			t.Error("turning auto-check off disables the delay and saves it")
		}
	})
}

func TestGeneralSaveFailure(t *testing.T) {
	b := newFakeBackend()
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		b.generalErr = errors.New("DatabaseQueryFailed", "disk full")
		d.fontSize.SetValue(20)
		if d.banner.IsHidden() || len(h.failed) != 1 || h.changed != 0 {
			t.Errorf("banner hidden=%v failed=%d changed=%d", d.banner.IsHidden(), len(h.failed), h.changed)
		}
	})
}

func TestTestConnectionFromTheList(t *testing.T) {
	tests := []struct {
		name string
		res  analysis.TestResult
		err  error
		want string
	}{
		{"success", analysis.TestResult{Elapsed: 1500 * time.Millisecond}, nil, "Connected. The LLM answered in 1.5 s."},
		{"failure", analysis.TestResult{}, errors.New("LLMCallFailed", "The request to local failed: connection refused"), "Failed: The request to local failed: connection refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newFakeBackend(ollamaCfg(1, "local"))
			b.testResult, b.testErr = tt.res, tt.err
			var d *PreferencesDialog
			uitest.OnUI(func() {
				d = NewPreferencesDialog(context.Background(), nil, b, PreferencesHooks{})
				d.testBtn.Click()
				if d.testResult.Text() != "Testing..." {
					t.Errorf("immediately after the click the label says %q", d.testResult.Text())
				}
			})
			waitLabel(t, func() string { return d.testResult.Text() }, tt.want)
			uitest.OnUI(func() { d.dlg.Delete() })
			if len(b.testedConfigs()) != 1 || b.testedConfigs()[0].Name != "local" {
				t.Errorf("tested = %+v", b.testedConfigs())
			}
		})
	}
}

func TestTestConnectionDoesNotBlockTheUI(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	b.testGate = make(chan struct{})
	var d *PreferencesDialog
	uitest.OnUI(func() {
		d = NewPreferencesDialog(context.Background(), nil, b, PreferencesHooks{})
		d.testBtn.Click() // returns at once although the backend is still waiting
	})
	// The UI thread is free: another call gets through while the test runs.
	uitest.OnUI(func() {
		if d.testResult.Text() != "Testing..." {
			t.Errorf("label = %q", d.testResult.Text())
		}
	})
	close(b.testGate)
	waitLabel(t, func() string { return d.testResult.Text() }, "Connected")
	uitest.OnUI(func() { d.dlg.Delete() })
}

func TestClosingTheDialogDropsALateTestResult(t *testing.T) {
	b := newFakeBackend(ollamaCfg(1, "local"))
	b.testGate = make(chan struct{})
	uitest.OnUI(func() {
		d := NewPreferencesDialog(context.Background(), nil, b, PreferencesHooks{})
		d.testBtn.Click()
		d.dlg.Accept() // closes: the context is cancelled and widgets may go away
		d.dlg.Delete()
	})
	close(b.testGate)
	// Give the goroutine time to post its result; nothing may touch the deleted widgets.
	time.Sleep(50 * time.Millisecond)
	uitest.OnUI(func() {})
}

func TestFormTestConnection(t *testing.T) {
	b := newFakeBackend()
	b.testResult = analysis.TestResult{Elapsed: 800 * time.Millisecond}
	withPrefs(t, b, func(d *PreferencesDialog, _ *hookLog) {
		var f *LLMForm
		d.execForm = func(form *LLMForm) {
			f = form
			form.provider.SetCurrentIndex(indexOfProvider("ollama"))
			form.model.SetText("llama3.1")
			form.testBtn.Click()
			if form.testResult.Text() != "Testing..." {
				t.Errorf("label = %q", form.testResult.Text())
			}
			// Leave the form open for the result; the test closes it below.
			form.dlg.Reject()
		}
		d.addBtn.Click()
		_ = f
	})
	time.Sleep(20 * time.Millisecond)
	if len(b.testedConfigs()) != 1 || b.testedConfigs()[0].Name != "this LLM" || b.testedConfigs()[0].Model != "llama3.1" {
		t.Errorf("tested = %+v: the form's current values are tested, with a placeholder name", b.testedConfigs())
	}
}

func TestFormTestConnectionNeedsAValidForm(t *testing.T) {
	b := newFakeBackend()
	withPrefs(t, b, func(d *PreferencesDialog, _ *hookLog) {
		d.execForm = func(f *LLMForm) {
			f.model.SetText("")
			f.testBtn.Click()
			if f.message.IsHidden() || !strings.Contains(f.message.Text(), "Model is required") {
				t.Errorf("message = %q", f.message.Text())
			}
			f.dlg.Reject()
		}
		d.addBtn.Click()
		if len(b.testedConfigs()) != 0 {
			t.Error("an invalid form must not be tested")
		}
	})
}

func TestFormatTestResult(t *testing.T) {
	tests := []struct {
		name   string
		res    analysis.TestResult
		err    error
		want   string
		wantOK bool
	}{
		{"ok", analysis.TestResult{Elapsed: 2340 * time.Millisecond}, nil, "Connected. The LLM answered in 2.3 s.", true},
		{"cancelled shows nothing", analysis.TestResult{Cancelled: true}, nil, "", true},
		{"app error", analysis.TestResult{}, errors.New("LLMCallTimeout", "took too long"), "Failed: took too long", false},
		{"plain error", analysis.TestResult{}, context.DeadlineExceeded, "Failed: context deadline exceeded", false},
	}
	for _, tt := range tests {
		got, ok := formatTestResult(tt.res, tt.err)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("%s: formatTestResult() = %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestFormShowsThePrivacyNoteForRemoteServers(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		d.execForm = func(f *LLMForm) {
			f.provider.SetCurrentIndex(indexOfProvider("ollama"))
			if !f.privacyNote.IsHidden() {
				t.Errorf("a local Ollama server needs no note, got %q", f.privacyNote.Text())
			}
			f.provider.SetCurrentIndex(indexOfProvider("anthropic"))
			if f.privacyNote.IsHidden() || !strings.Contains(f.privacyNote.Text(), "Anthropic's server") {
				t.Errorf("Anthropic: %q (hidden %v)", f.privacyNote.Text(), f.privacyNote.IsHidden())
			}
			f.provider.SetCurrentIndex(indexOfProvider("openai-compatible"))
			f.baseURL.SetText("http://localhost:1234/v1")
			if !f.privacyNote.IsHidden() {
				t.Errorf("a local OpenAI-compatible server needs no note, got %q", f.privacyNote.Text())
			}
			f.baseURL.SetText("https://api.openai.com/v1")
			if f.privacyNote.IsHidden() || !strings.Contains(f.privacyNote.Text(), "api.openai.com") {
				t.Errorf("remote server: %q (hidden %v)", f.privacyNote.Text(), f.privacyNote.IsHidden())
			}
			f.dlg.Reject()
		}
		d.addBtn.Click()
	})
}
