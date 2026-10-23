package web

import (
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
)

type modalKind int

// The things the page can show over the editor.
const (
	modalPrompt modalKind = iota + 1
	modalLibrary
	modalPrefs
	modalStats
)

// modal is what is shown over the editor, if anything. Its data is read when it
// is opened or changed, so drawing it needs no database access.
type modal struct {
	kind    modalKind
	prompt  *prompt
	library *libraryView
	prefs   *prefsView
	stats   *statsView
}

func (w *workspace) show(m *modal) {
	w.modal = m
	w.touch(rModal)
}

func (w *workspace) closeModal() {
	w.modal = nil
	w.touch(rModal)
}

// --- a question with one text field ---

type promptPurpose int

const (
	promptSaveAs promptPurpose = iota + 1
	promptLibraryTitle
	promptRename
)

// prompt asks for a file name or a title.
type prompt struct {
	purpose promptPurpose
	Title   string
	Label   string
	Value   string
	Button  string
	docID   uint   // the document a rename is for
	Error   string // why the last answer was refused
}

func (w *workspace) openPrompt(p *prompt) { w.show(&modal{kind: modalPrompt, prompt: p}) }

// submitPrompt handles the answer.
func (w *workspace) submitPrompt(value string) {
	if w.modal == nil || w.modal.kind != modalPrompt {
		return
	}
	p := w.modal.prompt
	value = strings.TrimSpace(value)
	p.Value = value
	if value == "" {
		p.Error = "Enter a " + strings.ToLower(p.Label) + "."
		w.touch(rModal)
		return
	}
	switch p.purpose {
	case promptSaveAs:
		w.closeModal()
		w.sendFile(safeFileName(value))
	case promptLibraryTitle:
		if err := w.createDocument(value); err != nil {
			w.logError("saving to the library", err)
			p.Error = userMessage(err)
			w.touch(rModal)
			return
		}
		w.closeModal()
	case promptRename:
		if err := w.libraryRename(p.docID, value); err != nil {
			w.logError("renaming a library document", err)
			p.Error = userMessage(err)
			w.touch(rModal)
			return
		}
		w.openLibrary()
	}
}

// --- the library ---

type libraryView struct {
	Docs []store.Document
}

func (w *workspace) openLibrary() {
	docs, err := w.srv.deps.Store.ListDocuments(w.ctx())
	if err != nil {
		w.showError("reading the library", err)
		return
	}
	w.show(&modal{kind: modalLibrary, library: &libraryView{Docs: docs}})
}

// refreshLibrary reloads the list when it is shown.
func (w *workspace) refreshLibrary() {
	if w.modal != nil && w.modal.kind == modalLibrary {
		w.openLibrary()
	}
}

// startRename asks for the new title of a document.
func (w *workspace) startRename(id uint) {
	doc, err := w.srv.deps.Store.GetDocument(w.ctx(), id)
	if err != nil {
		w.showError("renaming a library document", err)
		w.refreshLibrary()
		return
	}
	w.openPrompt(&prompt{
		purpose: promptRename, Title: "Rename document", Label: "Title",
		Value: doc.Title, Button: "Rename", docID: id,
	})
}

// --- Preferences ---

type prefsView struct {
	Tab     string // "llms" or "general"
	LLMs    []store.LLMConfig
	Edit    *llmForm // the Add / Edit form, or nil while the list is shown
	General generalForm
	Message string // the last failure
}

// generalForm holds the settings of the General tab.
type generalForm struct {
	AutoCheck bool
	DelayS    int
	Fonts     store.Fonts
}

// maxFontFamily is the longest font family name the General tab takes, in characters.
const maxFontFamily = 100

// llmForm is the Add / Edit form of one LLM. The API key stored for an LLM is
// never sent to the browser: hasKey says there is one, and an empty key field
// keeps it.
type llmForm struct {
	ID          uint
	Name        string
	Provider    string
	Model       string
	BaseURL     string
	APIKey      string // what was typed
	HasKey      bool
	ClearKey    bool
	Temperature string
	Timeout     string // seconds; empty is the default
	Parallel    string
	NoThinking  bool
	Error       string
	TestText    string
	TestOK      bool
	testSeq     uint64
}

const defaultOllamaURL = "http://localhost:11434"

func (f *llmForm) ollama() bool { return f.Provider == string(llm.ProviderOllama) }

// config is the configuration the form describes. stored is the one it edits (or
// nil for a new one); it supplies the API key that was not typed again. Fields
// the provider does not use are cleared.
func (f *llmForm) config(stored *store.LLMConfig) (store.LLMConfig, error) {
	cfg := store.LLMConfig{
		ID:       f.ID,
		Name:     strings.TrimSpace(f.Name),
		Provider: f.Provider,
		Model:    strings.TrimSpace(f.Model),
		BaseURL:  strings.TrimSpace(f.BaseURL),
		APIKey:   f.APIKey,
	}
	if cfg.APIKey == "" && stored != nil && !f.ClearKey {
		cfg.APIKey = stored.APIKey
	}
	if f.ollama() {
		cfg.APIKey = ""
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(f.Temperature), 64)
	if err != nil {
		return cfg, errors.New("LLMConfigInvalid", "Temperature must be a number between 0 and 2.", "field", "temperature")
	}
	cfg.Temperature = t
	if s := strings.TrimSpace(f.Timeout); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return cfg, errors.New("LLMConfigInvalid", "Timeout must be a number of seconds, or empty for the default.", "field", "timeout")
		}
		cfg.TimeoutSeconds = n
	}
	cfg.NoThinking = f.NoThinking && llm.SupportsNoThinking(llm.Provider(f.Provider))
	if s := strings.TrimSpace(f.Parallel); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return cfg, errors.New("LLMConfigInvalid", "Parallel requests must be a whole number.", "field", "max_parallel")
		}
		cfg.MaxParallel = n
	}
	return cfg, nil
}

func (w *workspace) openPrefs(tab string) {
	pv := &prefsView{Tab: tab}
	if !w.loadPrefs(pv) {
		return
	}
	w.show(&modal{kind: modalPrefs, prefs: pv})
}

// loadPrefs reads what the Preferences tabs show.
func (w *workspace) loadPrefs(pv *prefsView) bool {
	st := w.srv.deps.Store
	llms, err := st.ListLLMConfigs(w.ctx())
	if err != nil {
		w.showError("reading the LLM list", err)
		return false
	}
	pv.LLMs = llms
	pv.General = generalForm{AutoCheck: w.autoCheck, DelayS: int(w.autoDelay.Seconds()), Fonts: w.fonts}
	return true
}

func (w *workspace) prefsView() (*prefsView, bool) {
	if w.modal == nil || w.modal.kind != modalPrefs {
		return nil, false
	}
	return w.modal.prefs, true
}

func (w *workspace) prefsTab(tab string) {
	pv, ok := w.prefsView()
	if !ok || (tab != "llms" && tab != "general") {
		return
	}
	pv.Tab, pv.Edit, pv.Message = tab, nil, ""
	w.touch(rModal)
}

// editLLM opens the form for a configuration (id 0 adds a new one).
func (w *workspace) editLLM(id uint) {
	pv, ok := w.prefsView()
	if !ok {
		return
	}
	pv.Message = ""
	if id == 0 {
		pv.Edit = &llmForm{Provider: string(llm.DefaultProvider),
			Temperature: strconv.FormatFloat(store.DefaultTemperature, 'f', -1, 64), Parallel: "1", NoThinking: true}
	} else {
		cfg, err := w.srv.deps.Store.GetLLMConfig(w.ctx(), id)
		if err != nil {
			w.logError("opening an LLM", err)
			pv.Message = userMessage(err)
			w.touch(rModal)
			return
		}
		pv.Edit = &llmForm{
			ID: cfg.ID, Name: cfg.Name, Provider: cfg.Provider, Model: cfg.Model, BaseURL: cfg.BaseURL,
			HasKey: cfg.APIKey != "", Temperature: strconv.FormatFloat(cfg.Temperature, 'f', -1, 64),
			Timeout: timeoutText(cfg.TimeoutSeconds), Parallel: strconv.Itoa(max(cfg.MaxParallel, 1)), NoThinking: cfg.NoThinking,
		}
	}
	w.touch(rModal)
}

// formFromValues reads the fields of the LLM form.
func formFromValues(prev *llmForm, v func(string) string) *llmForm {
	return &llmForm{
		ID: prev.ID, HasKey: prev.HasKey,
		Name: v("name"), Provider: v("provider"), Model: v("model"), BaseURL: v("base_url"),
		APIKey: v("api_key"), ClearKey: v("clear_key") != "", Temperature: v("temperature"),
		Timeout: v("timeout"), Parallel: v("max_parallel"), NoThinking: v("no_thinking") != "",
	}
}

// llmFormChanged follows a change of the provider or the address: it applies
// the defaults of the provider the way the desktop form does (section 4.5).
func (w *workspace) llmFormChanged(v func(string) string) {
	pv, ok := w.prefsView()
	if !ok || pv.Edit == nil {
		return
	}
	prevProvider := pv.Edit.Provider
	f := formFromValues(pv.Edit, v)
	url := strings.TrimSpace(f.BaseURL)
	switch {
	case f.ollama() && url == "":
		f.BaseURL = defaultOllamaURL
	case !f.ollama() && prevProvider != f.Provider && url == defaultOllamaURL:
		// The default that was filled in for Ollama must not be sent to another
		// provider's configuration.
		f.BaseURL = ""
	}
	pv.Edit = f
	w.touch(rModal)
}

// saveLLM stores the form.
func (w *workspace) saveLLM(v func(string) string) {
	pv, ok := w.prefsView()
	if !ok || pv.Edit == nil {
		return
	}
	f := formFromValues(pv.Edit, v)
	pv.Edit = f
	st := w.srv.deps.Store
	var stored *store.LLMConfig
	if f.ID != 0 {
		cur, err := st.GetLLMConfig(w.ctx(), f.ID)
		if err != nil {
			f.Error = userMessage(err)
			w.touch(rModal)
			return
		}
		stored = cur
	}
	cfg, err := f.config(stored)
	if err == nil {
		err = cfg.Validate()
	}
	if err == nil {
		if cfg.ID == 0 {
			err = st.CreateLLMConfig(w.ctx(), &cfg)
		} else {
			err = st.UpdateLLMConfig(w.ctx(), &cfg)
		}
	}
	if err != nil {
		f.Error = userMessage(err)
		w.touch(rModal)
		return
	}
	pv.Edit = nil
	w.settingsChanged(pv)
}

// deleteLLM removes a configuration.
func (w *workspace) deleteLLM(id uint) {
	pv, ok := w.prefsView()
	if !ok {
		return
	}
	if err := w.srv.deps.Store.DeleteLLMConfig(w.ctx(), id); err != nil {
		w.logError("deleting an LLM", err)
		pv.Message = userMessage(err)
		w.touch(rModal)
		return
	}
	pv.Edit = nil
	w.settingsChanged(pv)
}

// settingsChanged follows a change of the LLM list: the selector is refilled,
// and the desktop window is told. Deleting the selected LLM selects another one
// and remembers the choice (section 4.5).
func (w *workspace) settingsChanged(pv *prefsView) {
	pv.Message = ""
	w.loadPrefs(pv)
	w.reloadLLMs()
	st := w.srv.deps.Store
	if remembered, err := st.SelectedLLMID(w.ctx()); err == nil && remembered != w.llmID {
		stillThere := false
		for _, c := range w.llms {
			stillThere = stillThere || c.ID == remembered
		}
		if !stillThere {
			if err := st.SetSelectedLLMID(w.ctx(), w.llmID); err != nil {
				w.logError("saving selected LLM", err)
			}
		}
	}
	w.touch(rModal | rToolbar)
	w.notifyDesktop()
}

func (w *workspace) notifyDesktop() {
	if f := w.srv.deps.SettingsChanged; f != nil {
		f()
	}
}

// testLLM sends a tiny request with the values in the form. The request runs in
// the background; its result is shown if the form is still open.
func (w *workspace) testLLM(v func(string) string) {
	pv, ok := w.prefsView()
	if !ok || pv.Edit == nil {
		return
	}
	f := formFromValues(pv.Edit, v)
	pv.Edit = f
	var stored *store.LLMConfig
	if f.ID != 0 {
		if cur, err := w.srv.deps.Store.GetLLMConfig(w.ctx(), f.ID); err == nil {
			stored = cur
		}
	}
	cfg, err := f.config(stored)
	if cfg.Name == "" {
		cfg.Name = "this LLM" // a test does not need a name
	}
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		f.Error, f.TestText = userMessage(err), ""
		w.touch(rModal)
		return
	}
	f.Error, f.TestText, f.TestOK = "", "Testing...", true
	f.testSeq++
	seq := f.testSeq
	w.touch(rModal)
	started := w.run.spawn(func() {
		res, err := analysis.TestConnection(w.ctx(), cfg.Name, cfg.ModelConfig(), 0)
		text, ok := formatTestResult(res, err)
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed {
			return
		}
		cur, open := w.prefsView()
		if !open || cur.Edit != f || f.testSeq != seq {
			return // the form was closed or tested again meanwhile
		}
		f.TestText, f.TestOK = text, ok
		w.touch(rModal)
	})
	if !started {
		f.TestText = ""
	}
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

// saveGeneral stores the General tab. Changes apply at once.
func (w *workspace) saveGeneral(v func(string) string) {
	pv, ok := w.prefsView()
	if !ok {
		return
	}
	g := generalForm{AutoCheck: v("auto_check") != ""}
	var msg string
	g.DelayS, msg = boundedInt(v("delay_s"), 1, 600, "The auto-check delay")
	for _, p := range []struct {
		font         *store.Font
		family, size string
		what         string
	}{
		{&g.Fonts.Editor, "editor_family", "font_size", "The editor font size"},
		{&g.Fonts.Buttons, "button_family", "button_size", "The buttons font size"},
		{&g.Fonts.Results, "results_family", "results_size", "The results font size"},
	} {
		if msg != "" {
			break
		}
		p.font.Size, msg = boundedInt(v(p.size), store.MinFontSize, store.MaxFontSize, p.what)
		if p.font.Family = strings.TrimSpace(v(p.family)); msg == "" && utf8.RuneCountInString(p.font.Family) > maxFontFamily {
			msg = fmt.Sprintf("A font name must be at most %d characters.", maxFontFamily)
		}
	}
	if msg != "" {
		pv.Message = msg
		w.touch(rModal)
		return
	}
	st := w.srv.deps.Store
	if err := st.SetAutoCheck(w.ctx(), g.AutoCheck); err != nil {
		w.showError("saving auto-check setting", err)
		return
	}
	if err := st.SetAutoCheckDelaySeconds(w.ctx(), g.DelayS); err != nil {
		w.showError("saving auto-check delay", err)
		return
	}
	if err := st.SetFonts(w.ctx(), g.Fonts); err != nil {
		w.showError("saving fonts", err)
		return
	}
	pv.General, pv.Message = g, ""
	w.autoDelay = secondsOf(g.DelayS)
	w.fonts = g.Fonts
	w.setAutoCheck(g.AutoCheck)
	w.touch(rModal | rSignals)
	w.notifyDesktop()
}

// setAutoCheckSetting is the Auto-check box of the toolbar: it is a setting, so
// it is stored like the box of the desktop window.
func (w *workspace) setAutoCheckSetting(on bool) {
	if err := w.srv.deps.Store.SetAutoCheck(w.ctx(), on); err != nil {
		w.logError("saving auto-check setting", err)
	}
	w.setAutoCheck(on)
	w.notifyDesktop()
}

// boundedInt reads a whole number in a range; msg says what is wrong when it is not.
func boundedInt(s string, lo, hi int, what string) (n int, msg string) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < lo || n > hi {
		return 0, fmt.Sprintf("%s must be a whole number from %d to %d.", what, lo, hi)
	}
	return n, ""
}

func secondsOf(s int) time.Duration { return time.Duration(s) * time.Second }

// --- Statistics ---

type statsView struct {
	Tab      string // "mistakes" or "usage"
	rows     []store.TypeStat
	filter   view.StatsFilter
	sort     view.StatsSort
	Selected string // the error type whose examples are shown
	examples []store.Example
	Usage    []view.UsageRow
	Error    string
}

func (w *workspace) openStats() {
	sv := &statsView{Tab: "mistakes", sort: view.DefaultStatsSort}
	if !w.loadStats(sv) {
		return
	}
	w.show(&modal{kind: modalStats, stats: sv})
}

// loadStats reads the counts and the token totals. They are small grouped
// queries over a local file.
func (w *workspace) loadStats(sv *statsView) bool {
	stats := w.srv.deps.Stats
	rows, err := stats.TypeStats(w.ctx())
	if err != nil {
		w.showError("reading statistics", err)
		return false
	}
	var totals [3][]store.UsageTotal
	for i, p := range []store.Period{store.AllTime, store.Last30Days, store.Last7Days} {
		if totals[i], err = stats.UsageTotals(w.ctx(), p); err != nil {
			w.showError("reading token usage", err)
			return false
		}
	}
	sv.rows = rows
	sv.Usage = view.UsageRows(totals[0], totals[1], totals[2])
	return true
}

func (w *workspace) statsView() (*statsView, bool) {
	if w.modal == nil || w.modal.kind != modalStats {
		return nil, false
	}
	return w.modal.stats, true
}

func (w *workspace) statsTab(tab string) {
	sv, ok := w.statsView()
	if !ok || (tab != "mistakes" && tab != "usage") {
		return
	}
	sv.Tab = tab
	w.touch(rModal)
}

// statsFilter applies the category filter and the period selector. The examples
// list is emptied by a filter or sort change (section 4.6).
func (w *workspace) statsFilter(category, period string) {
	sv, ok := w.statsView()
	if !ok {
		return
	}
	sv.filter.Category = ""
	for _, c := range analysis.Categories() {
		if string(c) == category {
			sv.filter.Category = category
		}
	}
	switch period {
	case "30":
		sv.filter.Period = store.Last30Days
	case "7":
		sv.filter.Period = store.Last7Days
	default:
		sv.filter.Period = store.AllTime
	}
	sv.Selected, sv.examples = "", nil
	w.touch(rModal)
}

// statsSort is a click on a column header.
func (w *workspace) statsSort(col int) {
	sv, ok := w.statsView()
	if !ok || col < int(view.ColType) || col > int(view.ColLastSeen) {
		return
	}
	sv.sort = view.NextSort(sv.sort, view.StatsColumn(col))
	sv.Selected, sv.examples = "", nil
	w.touch(rModal)
}

// statsSelect shows the three most recent examples of an error type.
func (w *workspace) statsSelect(errorType string) {
	sv, ok := w.statsView()
	if !ok {
		return
	}
	ex, err := w.srv.deps.Stats.RecentExamples(w.ctx(), errorType, 3)
	if err != nil {
		w.logError("reading examples", err)
		sv.Error = userMessage(err)
		w.touch(rModal)
		return
	}
	sv.Selected, sv.examples, sv.Error = errorType, ex, ""
	w.touch(rModal)
}

// statsReset deletes the recorded mistakes or the token usage. The row badges and
// the panel header are cleared after a reset of the mistakes.
func (w *workspace) statsReset(which string) {
	sv, ok := w.statsView()
	if !ok {
		return
	}
	var err error
	switch which {
	case "usage":
		err = w.srv.deps.Stats.ResetUsage(w.ctx())
	default:
		err = w.srv.deps.Stats.ResetMistakes(w.ctx())
	}
	if err != nil {
		w.logError("resetting statistics", err)
		sv.Error = userMessage(err)
		w.touch(rModal)
		return
	}
	sv.Error, sv.Selected, sv.examples = "", "", nil
	if !w.loadStats(sv) {
		return
	}
	if which != "usage" {
		w.typeCounts = map[string]int{}
		w.touch(rIssues)
		if sel, ok := w.state.Selected(); ok {
			w.loadHeaderCounts(w.explainSeq, sel)
		}
	}
	w.touch(rModal)
}

// timeoutText is the value of the timeout field: empty for the default.
func timeoutText(seconds int) string {
	if seconds == 0 {
		return ""
	}
	return strconv.Itoa(seconds)
}
