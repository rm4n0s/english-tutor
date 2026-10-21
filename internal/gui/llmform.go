package gui

import (
	"context"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
)

// apiKeyNote is shown next to the API key field (section 8 of the spec).
const apiKeyNote = "The key is stored in plain text in ~/.english-tutor/english-tutor.db. Only the file permissions (0600) protect it."

// NoThinkingNote explains the No thinking check box.
const NoThinkingNote = "Ask the model to answer at once, without thinking first. This is much faster for models that think. " +
	"It adds the fields that GLM, DeepSeek, Qwen and Ollama models use for it to each request. " +
	"If the server refuses them, switch it off. Anthropic models do not think unless asked, so it does nothing there."

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

// defaultOllamaURL is filled in when the provider is ollama and the field is empty.
const defaultOllamaURL = "http://localhost:11434"

// LLMForm is the dialog that adds or edits one LLM configuration (section 4.5).
type LLMForm struct {
	dialogBase
	ctx     context.Context
	rt      *Runtime
	backend PreferencesBackend
	orig    store.LLMConfig
	onSave  func(*store.LLMConfig) error

	name, model, baseURL, apiKey *widgets.TextEditingController
	provider                     string
	temperature                  spin
	timeout, parallel            spin
	noThinking                   bool
	keyShown                     bool

	message    string
	testResult string
	testOK     bool

	saved   bool
	savedID uint
	closed  bool
	// Done is called once, when the form is gone, with whether it was saved.
	done func(saved bool, id uint)
}

// NewLLMForm builds the form for cfg (a new configuration when cfg.ID is 0).
// onSave stores the configuration; an error it returns is shown in the form and
// keeps it open. done is called when the form is gone. Show it with
// win.ShowDialog.
func NewLLMForm(ctx context.Context, rt *Runtime, backend PreferencesBackend, cfg store.LLMConfig,
	onSave func(*store.LLMConfig) error, done func(saved bool, id uint)) *LLMForm {
	f := &LLMForm{dialogBase: newDialogBase(), ctx: ctx, rt: rt, backend: backend, orig: cfg, onSave: onSave, done: done,
		name: widgets.NewTextEditingController(cfg.Name), model: widgets.NewTextEditingController(cfg.Model),
		baseURL: widgets.NewTextEditingController(cfg.BaseURL), apiKey: widgets.NewTextEditingController(cfg.APIKey),
		temperature: spin{value: cfg.Temperature, min: 0, max: 2, step: 0.1, decimals: 2},
		timeout:     spin{value: float64(cfg.TimeoutSeconds), min: 0, max: store.MaxTimeoutSeconds, step: 30, suffix: " s", special: "Default"},
		parallel:    spin{value: float64(max(cfg.MaxParallel, 1)), min: 1, max: store.MaxParallelLimit, step: 1},
		noThinking:  cfg.NoThinking,
		provider:    cfg.Provider,
	}
	if f.provider == "" || indexOfProvider(f.provider) < 0 {
		f.provider = string(llm.DefaultProvider)
	}
	f.baseURL.AddListener(f.refresh)
	f.providerChanged()
	return f
}

func indexOfProvider(name string) int {
	for i, p := range llm.Providers() {
		if string(p) == name {
			return i
		}
	}
	return -1
}

// providerChanged updates what the fields say and allow for the provider.
func (f *LLMForm) providerChanged() {
	url := strings.TrimSpace(f.baseURL.Text())
	switch {
	case f.provider == string(llm.ProviderOllama) && url == "":
		f.baseURL.SetText(defaultOllamaURL)
	case f.provider != string(llm.ProviderOllama) && url == defaultOllamaURL:
		// The default that was filled in for Ollama must not be sent to
		// another provider's configuration.
		f.baseURL.SetText("")
	}
	f.refresh()
}

// SetProvider picks the provider.
func (f *LLMForm) SetProvider(p string) {
	if indexOfProvider(p) >= 0 {
		f.provider = p
		f.providerChanged()
	}
}

// ProviderName returns the provider shown.
func (f *LLMForm) ProviderName() string { return f.provider }

// ProviderNote returns the line that explains the Base URL for the provider.
func (f *LLMForm) ProviderNote() string { _, n := providerHints(f.provider); return n }

// PrivacyNoteText returns the note about sending text to a server on the
// internet, or "" when there is none.
func (f *LLMForm) PrivacyNoteText() string {
	return view.PrivacyNote(f.provider, f.baseURL.Text())
}

// KeyEnabled reports whether the API key can be typed: Ollama has none.
func (f *LLMForm) KeyEnabled() bool { return f.provider != string(llm.ProviderOllama) }

// NoThinkingEnabled reports whether the No thinking check box applies.
func (f *LLMForm) NoThinkingEnabled() bool { return llm.SupportsNoThinking(llm.Provider(f.provider)) }

// SetNoThinking ticks or unticks No thinking.
func (f *LLMForm) SetNoThinking(on bool) { f.noThinking = on; f.refresh() }

// SetKeyShown shows the API key as text, or masks it again. The key is always
// masked when the form opens.
func (f *LLMForm) SetKeyShown(on bool) { f.keyShown = on; f.refresh() }

// KeyShown reports whether the key is shown as text.
func (f *LLMForm) KeyShown() bool { return f.keyShown }

// SetTemperature, SetTimeout and SetParallel set the number fields.
func (f *LLMForm) SetTemperature(v float64) { f.temperature.value = min(max(v, 0), 2); f.refresh() }
func (f *LLMForm) SetTimeout(v int) {
	f.timeout.value = min(max(float64(v), 0), store.MaxTimeoutSeconds)
	f.refresh()
}
func (f *LLMForm) SetParallel(v int) {
	f.parallel.value = min(max(float64(v), 1), store.MaxParallelLimit)
	f.refresh()
}

// Config returns the configuration the form currently describes. Fields the
// provider does not use are cleared, so an old API key typed for another
// provider is not stored for an Ollama server.
func (f *LLMForm) Config() store.LLMConfig {
	cfg := f.orig
	cfg.Name = strings.TrimSpace(f.name.Text())
	cfg.Provider = f.provider
	cfg.Model = strings.TrimSpace(f.model.Text())
	cfg.BaseURL = strings.TrimSpace(f.baseURL.Text())
	cfg.APIKey = f.apiKey.Text()
	if cfg.Provider == string(llm.ProviderOllama) {
		cfg.APIKey = ""
	}
	cfg.Temperature = f.temperature.value
	cfg.TimeoutSeconds = int(f.timeout.value)
	cfg.MaxParallel = int(f.parallel.value)
	cfg.NoThinking = f.noThinking && llm.SupportsNoThinking(llm.Provider(cfg.Provider))
	return cfg
}

// Saved reports whether the form was saved.
func (f *LLMForm) Saved() bool { return f.saved }

// SavedID returns the ID of the saved configuration.
func (f *LLMForm) SavedID() uint { return f.savedID }

// Message returns the error shown in the form, or "".
func (f *LLMForm) Message() string { return f.message }

// TestResult returns the line after Test connection.
func (f *LLMForm) TestResult() string { return f.testResult }

// Save validates and stores the configuration and closes the form.
func (f *LLMForm) Save() {
	cfg := f.Config()
	if err := cfg.Validate(); err != nil {
		f.message = userMessage(err)
		f.refresh()
		return
	}
	if err := f.onSave(&cfg); err != nil {
		f.message = userMessage(err)
		f.refresh()
		return
	}
	f.saved, f.savedID = true, cfg.ID
	f.Close()
}

// Test checks the connection with the configuration as it is now.
func (f *LLMForm) Test() {
	cfg := f.Config()
	if cfg.Name == "" {
		cfg.Name = "this LLM" // a test does not need a name
	}
	if err := cfg.Validate(); err != nil {
		f.message = userMessage(err)
		f.refresh()
		return
	}
	f.message = ""
	f.testResult, f.testOK = "Testing...", true
	f.refresh()
	startTest(f.ctx, f.rt, f.backend, cfg, func() bool { return !f.closed }, func(text string, ok bool) {
		f.testResult, f.testOK = text, ok
		f.refresh()
	})
}

// Dismissed reports the result.
func (f *LLMForm) Dismissed() {
	f.open, f.closed = false, true
	if f.done != nil {
		f.done(f.saved, f.savedID)
	}
}

// Build draws the form.
func (f *LLMForm) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return f.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		title := "Edit LLM"
		if f.orig.ID == 0 {
			title = "Add LLM"
		}
		placeholder, providerNote := providerHints(f.provider)
		items := make([]*m.DropdownMenuItem[string], 0, len(llm.Providers()))
		for _, p := range llm.Providers() {
			items = append(items, m.NewDropdownMenuItem[string](m.NewText(string(p)), m.DropdownMenuItemOpts[string]{Value: foundation.Some(string(p))}))
		}
		provider := m.NewDropdownButton(items, func(v foundation.Opt[string]) {
			if p, ok := v.Get(); ok {
				f.SetProvider(p)
			}
		}, m.DropdownButtonOpts[string]{Value: foundation.Some(f.provider), IsExpanded: true})

		keyKind := m.TextFieldOpts{ObscureText: !f.keyShown, Enabled: foundation.Some(f.KeyEnabled())}
		keyRow := m.NewRow([]widgets.Widget{
			m.NewExpanded(textInput(f.apiKey, "", keyKind)),
			spaceX(6),
			tipped("Show or hide the key while you check it", button(map[bool]string{true: "Hide", false: "Show"}[f.keyShown], func() { f.SetKeyShown(!f.keyShown) })),
		})
		var noThinking func(bool)
		if f.NoThinkingEnabled() {
			noThinking = f.SetNoThinking
		}
		rows := []widgets.Widget{
			formRow("Name", textInput(f.name, "Shown in the selector, for example Claude Sonnet", m.TextFieldOpts{})),
			formRow("Provider", provider),
			note(bc, providerNote, false),
		}
		if p := f.PrivacyNoteText(); p != "" {
			rows = append(rows, note(bc, p, true))
		}
		rows = append(rows,
			formRow("Model", textInput(f.model, "for example llama3.1, gpt-4o or claude-sonnet-5-5", m.TextFieldOpts{})),
			formRow("Base URL", textInput(f.baseURL, placeholder, m.TextFieldOpts{})),
			formRow("API key", keyRow),
			note(bc, apiKeyNote, false),
			formRow("Temperature", f.temperature.widget(func(v float64) { f.SetTemperature(v) })),
			tipped("How long one request may take before it is given up. Default: 15 minutes for a check, 120 s for an explanation or alternatives. Use a longer time for slow models.",
				formRow("Timeout", f.timeout.widget(func(v float64) { f.SetTimeout(int(v)) }))),
			tipped("How many parts of a long text are checked at the same time. 1 sends them one after another. A higher number is faster but may hit the rate limit of the provider.",
				formRow("Parallel requests", f.parallel.widget(func(v float64) { f.SetParallel(int(v)) }))),
			tipped(NoThinkingNote, formRow("", checkRow("No thinking", f.noThinking && f.NoThinkingEnabled(), noThinking))),
		)
		if f.message != "" {
			rows = append(rows, errorLine(bc, f.message))
		}
		if f.testResult != "" {
			if f.testOK {
				rows = append(rows, m.NewSelectionArea(m.NewText(f.testResult)))
			} else {
				rows = append(rows, errorLine(bc, f.testResult))
			}
		}
		return frame(bc, title, 560, 640, m.NewSingleChildScrollView(m.NewColumn(rows, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})),
			button("Test connection", f.Test), m.NewExpanded(m.NewSizedBox(nil)),
			primary("Save", f.Save), spaceX(8), button("Cancel", f.Close))
	})
}
