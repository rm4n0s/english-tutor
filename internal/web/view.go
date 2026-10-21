package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"html"
	"html/template"
	"strings"
	"unicode/utf16"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

//go:embed templates/*.html
var templateFS embed.FS

// templates holds the HTML of the page, parsed once. html/template escapes
// everything that is put into it.
var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}).ParseFS(templateFS, "templates/*.html"))

func execute(name string, data any) string {
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, name, data); err != nil {
		// The templates are part of the program and are tested; an error here
		// is a bug, and the part of the page stays as it was.
		return "<!-- template " + name + " failed: " + html.EscapeString(err.Error()) + " -->"
	}
	return b.String()
}

// --- the page ---

type pageData struct {
	Title   string
	Signals string // the JSON of the initial signals
	Text    string
	Regions pageRegions
	Remote  bool
}

// pageRegions are the parts of the page that are also pushed later.
type pageRegions struct {
	Toolbar, Banner, Under, Issues, Explain, Status, Modal template.HTML
}

// signalsFor builds the signals that are not the text.
func (w *workspace) signals(sentGoto *int) map[string]any {
	m := map[string]any{
		"_title":    windowTitle(w.displayName(), w.modified()),
		"_modified": w.modified(),
		"_fs":       w.fontSize,
	}
	if sentGoto != nil && *sentGoto != w.gotoSeq {
		m["_goto"] = map[string]int{"seq": w.gotoSeq, "pos": w.gotoPos}
		*sentGoto = w.gotoSeq
	}
	return m
}

// pageHTML renders the whole page for a browser that has just arrived.
func (w *workspace) pageHTML(remote bool) string {
	sig := w.signals(nil)
	sig["text"] = w.text
	sig["_goto"] = map[string]int{"seq": 0, "pos": 0}
	b, err := json.Marshal(sig)
	if err != nil {
		b = []byte("{}")
	}
	return execute("page", pageData{
		Title:   windowTitle(w.displayName(), w.modified()),
		Signals: string(b),
		Text:    w.text,
		Remote:  remote,
		Regions: pageRegions{
			Toolbar: w.renderToolbar(remote), Banner: w.renderBanner(), Under: w.renderUnder(),
			Issues: w.renderIssues(), Explain: w.renderExplain(), Status: w.renderStatus(), Modal: w.renderModal(),
		},
	})
}

// render builds the patches for the parts of the page in r: the elements, as one
// HTML string, and the signals. The caller holds mu.
func (w *workspace) render(r regionSet, remote bool, sentGoto *int) (elements string, signals map[string]any) {
	var b strings.Builder
	add := func(set regionSet, f func() template.HTML) {
		if r&set != 0 {
			b.WriteString(string(f()))
		}
	}
	add(rToolbar, func() template.HTML { return w.renderToolbar(remote) })
	add(rBanner, w.renderBanner)
	add(rUnder, w.renderUnder)
	add(rIssues, w.renderIssues)
	add(rExplain, w.renderExplain)
	add(rStatus, w.renderStatus)
	add(rModal, w.renderModal)
	if r&rDownload != 0 {
		b.WriteString(w.renderDownload())
	}
	if r&(rSignals|rText) != 0 {
		signals = w.signals(sentGoto)
		if r&rText != 0 {
			signals["text"] = w.text
		}
	}
	return b.String(), signals
}

// --- toolbar ---

type toolbarData struct {
	LLMs     []llmChoice
	LLMID    uint
	Checking bool
	CanCheck bool
	Auto     bool
	Legend   []legendEntry
	Name     string
	Modified bool
	Remote   bool
}

type legendEntry struct {
	ID     string
	Label  string
	Sample string
	On     bool
}

func (w *workspace) renderToolbar(remote bool) template.HTML {
	d := toolbarData{
		LLMs: w.llms, LLMID: w.llmID, Checking: w.checking, CanCheck: w.llmID != 0,
		Auto: w.autoCheck, Name: w.displayName(), Modified: w.modified(), Remote: remote,
	}
	for _, c := range []analysis.Category{analysis.Grammar, analysis.Vocabulary, analysis.Syntax} {
		d.Legend = append(d.Legend, legendEntry{
			ID: string(c), Label: c.Label(), Sample: view.CategorySample(c), On: w.state.CategoryVisible(c),
		})
	}
	return template.HTML(execute("toolbar", d)) //nolint:gosec // rendered by html/template
}

// --- banner ---

func (w *workspace) renderBanner() template.HTML {
	return template.HTML(execute("banner", w.banner)) //nolint:gosec // rendered by html/template
}

// --- the layer under the editor ---

// renderUnder draws the underlines: the text again, invisible, with the issue
// ranges wrapped in marks that carry the colour and the line style. It sits
// exactly under the editor, which is transparent (section 4.2).
func (w *workspace) renderUnder() template.HTML {
	units := utf16.Encode([]rune(w.text))
	slice := func(a, b int) string { return string(utf16.Decode(units[a:b])) }
	sel, hasSel := w.state.Selected()

	var b strings.Builder
	b.WriteString(`<div id="underlay" aria-hidden="true">`)
	pos := 0
	for _, it := range w.state.Visible() {
		if it.Start < pos || it.End > len(units) || it.Start >= it.End {
			continue // an issue the text no longer has room for
		}
		b.WriteString(html.EscapeString(slice(pos, it.Start)))
		class := "u u-" + string(it.Category())
		if hasSel && sel.ID == it.ID {
			class += " sel"
		}
		fmt.Fprintf(&b, `<span class="%s" data-tip="%s">%s</span>`, class,
			html.EscapeString(tooltip(it)), html.EscapeString(slice(it.Start, it.End)))
		pos = it.End
	}
	b.WriteString(html.EscapeString(slice(pos, len(units))))
	// The editor shows a last empty line after a final newline; the layer needs
	// a character there to be as tall.
	b.WriteString("\u200b</div>")
	return template.HTML(b.String()) //nolint:gosec // every piece was escaped above
}

// tooltip is the tooltip of an underline: the error type, its category, and the
// short message (section 4.2).
func tooltip(it issues.Item) string {
	return fmt.Sprintf("%s · %s\n%s", it.Type.Label, it.Type.Category.Label(), it.Message)
}

// --- issues list ---

type issuesData struct {
	Header string
	Empty  string
	Rows   []issueRow
}

type issueRow struct {
	issues.Row
	Selected bool
}

func (w *workspace) renderIssues() template.HTML {
	d := issuesData{Header: w.state.Header(), Empty: w.state.EmptyText()}
	selected := 0
	if sel, ok := w.state.Selected(); ok {
		selected = sel.ID
	}
	for _, it := range w.state.Visible() {
		count := 0
		if w.resultRecorded {
			count = w.typeCounts[it.Type.ID]
		}
		d.Rows = append(d.Rows, issueRow{Row: issues.RowFor(it, count), Selected: it.ID == selected})
	}
	return template.HTML(execute("issues", d)) //nolint:gosec // rendered by html/template
}

// --- explanation panel ---

type explainData struct {
	Header     string
	Loading    bool
	CanCancel  bool
	RedoLabel  string
	NotAnError bool
	Hint       bool // the idle hint is the body
	NoLLM      bool // there is no LLM to ask
	Failed     bool // the body is an error message
	Body       template.HTML
	Plain      string // the body as text, for a failure
}

func (w *workspace) renderExplain() template.HTML {
	d := explainData{}
	switch w.explainState {
	case view.ExplainIdle:
		d.Hint = true
		d.NoLLM = len(w.llms) == 0
	case view.ExplainLoading:
		d.Header, d.Loading, d.CanCancel, d.NotAnError = w.explainHeader, true, true, true
		if w.explainBody == "" {
			d.Body = renderMarkdown("Explaining **" + w.explainTitle + "**...")
		} else {
			d.Body = renderMarkdown(w.explainBody)
		}
	case view.ExplainReady:
		d.Header, d.RedoLabel, d.NotAnError = w.explainHeader, "Regenerate", true
		d.Body = renderMarkdown(w.explainBody)
	case view.ExplainFailed:
		d.Header, d.RedoLabel, d.NotAnError = w.explainHeader, "Retry", true
		d.Failed, d.Plain = true, w.explainBody
	case view.ExplainCancelled:
		d.Header, d.RedoLabel, d.NotAnError = w.explainHeader, "Regenerate", true
		d.Body = renderMarkdown("Explanation cancelled.")
	}
	return template.HTML(execute("explain", d)) //nolint:gosec // rendered by html/template
}

// --- status bar ---

type statusData struct {
	State  string
	Words  string
	Tokens string
}

func (w *workspace) renderStatus() template.HTML {
	state := w.notice
	if state == "" {
		state = view.FormatStatus(w.status, w.state.Phase() == issues.Checked, len(w.state.Items()), w.unplaced)
	}
	return template.HTML(execute("status", statusData{ //nolint:gosec // rendered by html/template
		State:  state,
		Words:  view.FormatWords(analysis.WordCount(w.text)),
		Tokens: view.FormatTokens(w.lastUsage, w.sessionUsage),
	}))
}

// --- download ---

// renderDownload is the element the page watches: when it appears with a
// new address, the browser fetches the file.
func (w *workspace) renderDownload() string {
	if w.download == nil {
		return `<div id="download" hidden></div>`
	}
	return execute("download", map[string]string{"Href": "download/" + w.download.token, "Name": w.download.name})
}

// --- the dialogs ---

type modalData struct {
	Open    bool
	Kind    string
	Prompt  *prompt
	Library *libraryView
	Prefs   *prefsData
	Stats   *statsData
}

func (w *workspace) renderModal() template.HTML {
	d := modalData{}
	if m := w.modal; m != nil {
		d.Open = true
		switch m.kind {
		case modalPrompt:
			d.Kind, d.Prompt = "prompt", m.prompt
		case modalLibrary:
			d.Kind, d.Library = "library", m.library
		case modalPrefs:
			d.Kind, d.Prefs = "prefs", w.prefsData(m.prefs)
		case modalStats:
			d.Kind, d.Stats = "stats", w.statsData(m.stats)
		}
	}
	return template.HTML(execute("modal", d)) //nolint:gosec // rendered by html/template
}

type prefsData struct {
	*prefsView
	IsGeneral bool
	Form      *llmFormData
}

type llmFormData struct {
	*llmForm
	Title       string
	Placeholder string
	ProviderTip string
	Privacy     string
	KeyDisabled bool
	Providers   []selectOption
	APIKeyNote  string
	// NoThinkingDisabled is true for a provider the No thinking option does nothing for.
	NoThinkingNote     string
	NoThinkingDisabled bool
	// Limits of the Timeout and Parallel requests fields.
	MinTimeout, MaxTimeout, MaxParallel int
}

const apiKeyNote = "The key is stored in plain text in ~/.english-tutor/english-tutor.db. Only the file permissions (0600) protect it. It is never sent to this page."

// providerHints are the Base URL placeholder and the note shown for a provider.
func providerHints(p string) (placeholder, note string) {
	switch p {
	case "ollama":
		return "http://localhost:11434", "The address of your Ollama server."
	case "openai-compatible":
		return "https://api.openai.com/v1", "The address of any OpenAI-compatible API, including its /v1 part."
	default:
		return "https://api.anthropic.com (default)", "Optional: leave empty for Anthropic's own server."
	}
}

func (w *workspace) prefsData(pv *prefsView) *prefsData {
	d := &prefsData{prefsView: pv, IsGeneral: pv.Tab == "general"}
	if f := pv.Edit; f != nil {
		fd := &llmFormData{llmForm: f, Title: "Edit LLM", KeyDisabled: f.ollama(), APIKeyNote: apiKeyNote, NoThinkingNote: ui.NoThinkingNote,
			NoThinkingDisabled: !llm.SupportsNoThinking(llm.Provider(f.Provider)),
			MinTimeout:         store.MinTimeoutSeconds, MaxTimeout: store.MaxTimeoutSeconds, MaxParallel: store.MaxParallelLimit}
		if f.ID == 0 {
			fd.Title = "Add LLM"
		}
		for _, p := range llm.Providers() {
			fd.Providers = append(fd.Providers, selectOption{Value: string(p), Label: string(p), Selected: string(p) == f.Provider})
		}
		fd.Placeholder, fd.ProviderTip = providerHints(f.Provider)
		fd.Privacy = view.PrivacyNote(f.Provider, f.BaseURL)
		d.Form = fd
	}
	return d
}

type statsData struct {
	*statsView
	Mistakes   bool
	Summary    string
	Columns    []statsColumn
	Table      []statsRow
	Examples   []string
	Categories []selectOption
	Periods    []selectOption
	HasRows    bool
}

type statsColumn struct {
	Index int
	Title string
	Arrow string // "▲", "▼" or ""
}

type statsRow struct {
	Type     string
	Cells    []string
	Selected bool
}

type selectOption struct {
	Value, Label string
	Selected     bool
}

var statsColumnTitles = []string{"Error type", "Category", "Total", "Last 7 days", "Last 30 days", "Last seen"}

func (w *workspace) statsData(sv *statsView) *statsData {
	d := &statsData{statsView: sv, Mistakes: sv.Tab == "mistakes"}
	rows := view.FilterStats(sv.rows, sv.filter)
	view.SortStats(rows, sv.sort)
	d.Summary = view.StatsSummary(sv.rows, sv.filter)
	d.HasRows = len(sv.rows) > 0
	for i, title := range statsColumnTitles {
		c := statsColumn{Index: i, Title: title}
		if int(sv.sort.Column) == i {
			c.Arrow = "▲"
			if sv.sort.Descending {
				c.Arrow = "▼"
			}
		}
		d.Columns = append(d.Columns, c)
	}
	for _, r := range rows {
		cells := view.StatsCells(r)
		d.Table = append(d.Table, statsRow{Type: r.ErrorType, Cells: cells[:], Selected: r.ErrorType == sv.Selected})
	}
	for _, e := range sv.examples {
		d.Examples = append(d.Examples, view.FormatExample(e))
	}
	d.Categories = []selectOption{{"", "All categories", sv.filter.Category == ""}}
	for _, c := range analysis.Categories() {
		d.Categories = append(d.Categories, selectOption{string(c), c.Label(), sv.filter.Category == string(c)})
	}
	d.Periods = []selectOption{
		{"all", "All time", sv.filter.Period == store.AllTime},
		{"30", "Last 30 days", sv.filter.Period == store.Last30Days},
		{"7", "Last 7 days", sv.filter.Period == store.Last7Days},
	}
	return d
}
