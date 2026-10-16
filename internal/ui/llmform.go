package ui

import (
	"context"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// apiKeyNote is shown next to the API key field (section 8 of the spec).
const apiKeyNote = "The key is stored in plain text in ~/.english-tutor/english-tutor.db. Only the file permissions (0600) protect it."

// LLMForm is the dialog that adds or edits one LLM configuration (section 4.5).
type LLMForm struct {
	dlg     *qt.QDialog
	ctx     context.Context
	backend PreferencesBackend
	orig    store.LLMConfig
	onSave  func(*store.LLMConfig) error

	name, model, baseURL, apiKey *qt.QLineEdit
	showKey                      *qt.QPushButton
	provider                     *qt.QComboBox
	temperature                  *qt.QDoubleSpinBox
	providerNote                 *qt.QLabel
	privacyNote                  *qt.QLabel
	message                      *qt.QLabel
	testResult                   *qt.QLabel
	saveBtn                      *qt.QPushButton
	testBtn                      *qt.QPushButton

	saved   bool
	savedID uint
	closed  bool
}

// NewLLMForm builds the form for cfg (a new configuration when cfg.ID is 0).
// onSave stores the configuration; an error it returns is shown in the form
// and keeps it open.
func NewLLMForm(ctx context.Context, parent *qt.QWidget, backend PreferencesBackend, cfg store.LLMConfig, onSave func(*store.LLMConfig) error) *LLMForm {
	f := &LLMForm{ctx: ctx, backend: backend, orig: cfg, onSave: onSave}
	title := "Edit LLM"
	if cfg.ID == 0 {
		title = "Add LLM"
	}
	f.dlg = qt.NewQDialog(parent)
	f.dlg.SetWindowTitle(title)
	f.dlg.SetModal(true)
	f.dlg.Resize(520, 0)

	layout := qt.NewQVBoxLayout2()
	f.dlg.SetLayout(layout.QLayout)
	form := qt.NewQFormLayout2()
	layout.AddLayout(form.QLayout)

	f.name = qt.NewQLineEdit2()
	f.name.SetPlaceholderText("Shown in the selector, for example Claude Sonnet")
	form.AddRow3("Name", f.name.QWidget)

	f.provider = qt.NewQComboBox2()
	for _, p := range llm.Providers() {
		f.provider.AddItem(string(p))
	}
	form.AddRow3("Provider", f.provider.QWidget)
	f.providerNote = qt.NewQLabel3("")
	f.providerNote.SetWordWrap(true)
	form.AddRow3("", f.providerNote.QWidget)

	f.privacyNote = qt.NewQLabel3("")
	f.privacyNote.SetWordWrap(true)
	f.privacyNote.SetStyleSheet("font-style: italic;")
	form.AddRow3("", f.privacyNote.QWidget)

	f.model = qt.NewQLineEdit2()
	f.model.SetPlaceholderText("for example llama3.1, gpt-4o or claude-sonnet-5-5")
	form.AddRow3("Model", f.model.QWidget)

	f.baseURL = qt.NewQLineEdit2()
	form.AddRow3("Base URL", f.baseURL.QWidget)

	f.apiKey = qt.NewQLineEdit2()
	f.apiKey.SetEchoMode(qt.QLineEdit__Password)
	f.showKey = qt.NewQPushButton3("Show")
	f.showKey.SetCheckable(true)
	f.showKey.SetToolTip("Show or hide the key while you check it")
	keyRow := qt.NewQWidget2()
	keyLayout := qt.NewQHBoxLayout2()
	keyLayout.SetContentsMargins(0, 0, 0, 0)
	keyRow.SetLayout(keyLayout.QLayout)
	keyLayout.AddWidget2(f.apiKey.QWidget, 1)
	keyLayout.AddWidget(f.showKey.QWidget)
	form.AddRow3("API key", keyRow)
	keyNote := qt.NewQLabel3(apiKeyNote)
	keyNote.SetWordWrap(true)
	form.AddRow3("", keyNote.QWidget)

	f.temperature = qt.NewQDoubleSpinBox2()
	f.temperature.SetRange(0, 2)
	f.temperature.SetSingleStep(0.1)
	f.temperature.SetDecimals(2)
	form.AddRow3("Temperature", f.temperature.QWidget)

	f.message = qt.NewQLabel3("")
	f.message.SetWordWrap(true)
	f.message.SetStyleSheet("color: " + colorError + ";")
	f.message.Hide()
	layout.AddWidget(f.message.QWidget)
	f.testResult = qt.NewQLabel3("")
	f.testResult.SetWordWrap(true)
	layout.AddWidget(f.testResult.QWidget)

	buttons := qt.NewQHBoxLayout2()
	f.testBtn = qt.NewQPushButton3("Test connection")
	buttons.AddWidget(f.testBtn.QWidget)
	buttons.AddStretch()
	f.saveBtn = qt.NewQPushButton3("Save")
	f.saveBtn.SetDefault(true)
	cancel := qt.NewQPushButton3("Cancel")
	buttons.AddWidget(f.saveBtn.QWidget)
	buttons.AddWidget(cancel.QWidget)
	layout.AddLayout(buttons.QLayout)

	f.fill(cfg)
	f.provider.OnCurrentTextChanged(func(string) { f.providerChanged() })
	f.baseURL.OnTextChanged(func(string) { f.updatePrivacyNote() })
	f.showKey.OnToggled(f.keyVisible)
	f.saveBtn.OnClicked(f.save)
	f.testBtn.OnClicked(f.test)
	cancel.OnClicked(f.dlg.Reject)
	f.dlg.OnFinished(func(int) { f.closed = true })
	return f
}

func (f *LLMForm) fill(cfg store.LLMConfig) {
	f.name.SetText(cfg.Name)
	provider := cfg.Provider
	if provider == "" {
		provider = string(llm.ProviderOllama)
	}
	if i := indexOfProvider(provider); i >= 0 {
		f.provider.SetCurrentIndex(i)
	}
	f.model.SetText(cfg.Model)
	f.baseURL.SetText(cfg.BaseURL)
	f.apiKey.SetText(cfg.APIKey)
	f.temperature.SetValue(cfg.Temperature)
	f.providerChanged()
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
	p := f.provider.CurrentText()
	placeholder, note := providerHints(p)
	f.baseURL.SetPlaceholderText(placeholder)
	f.providerNote.SetText(note)
	f.apiKey.SetEnabled(p != string(llm.ProviderOllama))
	f.showKey.SetEnabled(p != string(llm.ProviderOllama))
	url := strings.TrimSpace(f.baseURL.Text())
	switch {
	case p == string(llm.ProviderOllama) && url == "":
		f.baseURL.SetText(defaultOllamaURL)
	case p != string(llm.ProviderOllama) && url == defaultOllamaURL:
		// The default that was filled in for Ollama must not be sent to
		// another provider's configuration.
		f.baseURL.SetText("")
	}
	f.updatePrivacyNote()
}

// keyVisible shows the API key as text, or masks it again. The key is always
// masked when the form opens.
func (f *LLMForm) keyVisible(on bool) {
	if on {
		f.apiKey.SetEchoMode(qt.QLineEdit__Normal)
		f.showKey.SetText("Hide")
		return
	}
	f.apiKey.SetEchoMode(qt.QLineEdit__Password)
	f.showKey.SetText("Show")
}

// updatePrivacyNote shows or hides the note about sending text to a server on
// the internet.
func (f *LLMForm) updatePrivacyNote() {
	note := PrivacyNote(f.provider.CurrentText(), f.baseURL.Text())
	f.privacyNote.SetText(note)
	f.privacyNote.SetVisible(note != "")
}

// Config returns the configuration the form currently describes. Fields the
// provider does not use are cleared, so an old API key typed for another
// provider is not stored for an Ollama server.
func (f *LLMForm) Config() store.LLMConfig {
	cfg := f.orig
	cfg.Name = strings.TrimSpace(f.name.Text())
	cfg.Provider = f.provider.CurrentText()
	cfg.Model = strings.TrimSpace(f.model.Text())
	cfg.BaseURL = strings.TrimSpace(f.baseURL.Text())
	cfg.APIKey = f.apiKey.Text()
	if cfg.Provider == string(llm.ProviderOllama) {
		cfg.APIKey = ""
	}
	cfg.Temperature = f.temperature.Value()
	return cfg
}

// Saved reports whether the form was saved.
func (f *LLMForm) Saved() bool { return f.saved }

// SavedID returns the ID of the saved configuration.
func (f *LLMForm) SavedID() uint { return f.savedID }

func (f *LLMForm) save() {
	cfg := f.Config()
	if err := cfg.Validate(); err != nil {
		f.showMessage(userMessage(err))
		return
	}
	if err := f.onSave(&cfg); err != nil {
		f.showMessage(userMessage(err))
		return
	}
	f.saved, f.savedID = true, cfg.ID
	f.dlg.Accept()
}

func (f *LLMForm) test() {
	cfg := f.Config()
	if cfg.Name == "" {
		cfg.Name = "this LLM" // a test does not need a name
	}
	if err := cfg.Validate(); err != nil {
		f.showMessage(userMessage(err))
		return
	}
	f.clearMessage()
	f.testResult.SetStyleSheet("")
	f.testResult.SetText("Testing...")
	startTest(f.ctx, f.backend, cfg, func() bool { return !f.closed }, func(text string, ok bool) {
		showResult(f.testResult, text, ok)
	})
}

func (f *LLMForm) showMessage(text string) {
	f.message.SetText(text)
	f.message.Show()
}

func (f *LLMForm) clearMessage() {
	f.message.SetText("")
	f.message.Hide()
}
