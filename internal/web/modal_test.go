package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

const secretKey = "sk-this-key-must-never-reach-the-browser"

func TestPreferencesListsLLMsWithoutTheirKeys(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	cfg := &store.LLMConfig{Name: "cloud", Provider: "openai-compatible", Model: "gpt-4o", BaseURL: "https://api.example.com/v1", APIKey: secretKey, Temperature: 0.2}
	if err := e.st.CreateLLMConfig(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	id, page := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")
	list := s.waitRegion(t, "modal", "Preferences", ">cloud<", "openai-compatible · gpt-4o", ">local<")
	b.post(id, "prefs/llm/edit?id="+uitoa(cfg.ID), "")
	form := s.waitRegion(t, "modal", "Edit LLM", `value="cloud"`, "A key is stored. Leave empty to keep it.")
	s.quiet(t)
	for _, ev := range s.all {
		if strings.Contains(ev.Data, secretKey) {
			t.Fatalf("the API key was sent to the browser: %q", ev.Data)
		}
	}
	if strings.Contains(page+list+form, secretKey) {
		t.Fatal("the API key was sent to the browser")
	}
	if !strings.Contains(form, "Privacy: the text you write is sent to api.example.com") {
		t.Errorf("the privacy note names the server: %q", form)
	}
	if !strings.Contains(form, `id="api-key" type="password"`) || !strings.Contains(form, `et.toggleKey(el)`) {
		t.Error("the key field starts masked and has a Show button for what is typed")
	}
	if !strings.Contains(form, `name="clear_key"`) {
		t.Error("a stored key can be removed")
	}
}

func formValues(name, provider, model, baseURL, key string) url.Values {
	return url.Values{"name": {name}, "provider": {provider}, "model": {model}, "base_url": {baseURL}, "api_key": {key}, "temperature": {"0.2"}}
}

func TestAddEditAndDeleteAnLLM(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")
	s.waitRegion(t, "modal", "Preferences")

	// Add. An invalid form stays open with the message.
	b.post(id, "prefs/llm/edit?id=0", "")
	s.waitRegion(t, "modal", "Add LLM", `<option value="openai-compatible" selected`)
	b.postForm(id, "prefs/llm/save", formValues("cloud", "anthropic", "claude-sonnet-5-5", "", ""))
	s.waitRegion(t, "modal", "API key is required for Anthropic.", "Add LLM")
	b.postForm(id, "prefs/llm/save", formValues("cloud", "anthropic", "claude-sonnet-5-5", "", secretKey))
	s.waitRegion(t, "modal", ">cloud<", "anthropic · claude-sonnet-5-5")
	s.waitRegion(t, "toolbar", ">cloud</option>")
	select {
	case <-e.changed:
	case <-time.After(time.Second):
		t.Error("the desktop window must be told")
	}
	cfgs, _ := e.st.ListLLMConfigs(t.Context())
	var cloud store.LLMConfig
	for _, c := range cfgs {
		if c.Name == "cloud" {
			cloud = c
		}
	}
	if cloud.APIKey != secretKey {
		t.Fatalf("the key was not stored: %+v", cloud)
	}

	// A duplicate name shows the store's message.
	b.post(id, "prefs/llm/edit?id=0", "")
	s.waitRegion(t, "modal", "Add LLM")
	b.postForm(id, "prefs/llm/save", formValues("cloud", "ollama", "llama3.1", "http://localhost:11434", ""))
	s.waitRegion(t, "modal", "already")
	b.post(id, "prefs/tab?tab=llms", "")

	// Edit with the key field empty keeps the key.
	b.post(id, "prefs/llm/edit?id="+uitoa(cloud.ID), "")
	s.waitRegion(t, "modal", "Edit LLM")
	b.postForm(id, "prefs/llm/save", formValues("cloud 2", "anthropic", "claude-sonnet-5-5", "", ""))
	s.waitRegion(t, "modal", ">cloud 2<")
	got, _ := e.st.GetLLMConfig(t.Context(), cloud.ID)
	if got.Name != "cloud 2" || got.APIKey != secretKey {
		t.Errorf("after editing: %+v", got)
	}
	// Removing the key is a choice of its own.
	b.post(id, "prefs/llm/edit?id="+uitoa(cloud.ID), "")
	s.waitRegion(t, "modal", "Edit LLM")
	form := formValues("cloud 2", "openai-compatible", "m", "https://api.example.com/v1", "")
	form.Set("clear_key", "1")
	b.postForm(id, "prefs/llm/save", form)
	s.waitRegion(t, "modal", "openai-compatible")
	if got, _ := e.st.GetLLMConfig(t.Context(), cloud.ID); got.APIKey != "" {
		t.Errorf("the key was not removed: %+v", got)
	}

	// Deleting the selected LLM selects another one and remembers the choice.
	b.post(id, "llm?id="+uitoa(cloud.ID), "")
	s.waitRegion(t, "toolbar", `value="`+uitoa(cloud.ID)+`" selected`)
	b.post(id, "prefs/llm/delete?id="+uitoa(cloud.ID), "")
	s.waitRegion(t, "toolbar", `value="`+uitoa(e.llmID)+`" selected`)
	if sel, _ := e.st.SelectedLLMID(t.Context()); sel != e.llmID {
		t.Errorf("the new choice is remembered: %d, want %d", sel, e.llmID)
	}
	b.post(id, "prefs/llm/delete?id=999", "")
	s.waitRegion(t, "modal", "no longer exists")
}

func TestProviderChangeAppliesDefaults(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")
	b.post(id, "prefs/llm/edit?id=0", "")
	s.waitRegion(t, "modal", "Add LLM", "The address of any OpenAI-compatible API")

	// Choosing Ollama fills in its address.
	b.postForm(id, "prefs/llm/form", formValues("x", "ollama", "m", "", ""))
	s.waitRegion(t, "modal", "The address of your Ollama server.", `value="http://localhost:11434"`)

	// Away from Ollama the default address is cleared; the key field opens up.
	b.postForm(id, "prefs/llm/form", formValues("x", "anthropic", "m", "http://localhost:11434", ""))
	form := s.waitRegion(t, "modal", "Optional: leave empty for Anthropic&#39;s own server.")
	if strings.Contains(form, `value="http://localhost:11434"`) {
		t.Error("an Anthropic configuration must not inherit the Ollama address")
	}
	if !strings.Contains(form, "Privacy: the text you write is sent to Anthropic&#39;s server") {
		t.Errorf("the privacy note names Anthropic: %q", form)
	}
	if strings.Contains(form, `name="api_key" autocomplete="new-password" disabled`) {
		t.Error("the key field is for Anthropic")
	}
	// Back to Ollama fills the address and disables the key.
	b.postForm(id, "prefs/llm/form", formValues("x", "ollama", "m", "", "typed"))
	form = s.waitRegion(t, "modal", "The address of your Ollama server.", `value="http://localhost:11434"`)
	if !strings.Contains(form, `autocomplete="new-password" disabled`) {
		t.Errorf("the key field is disabled for Ollama: %q", form)
	}
	if strings.Contains(form, "Privacy:") {
		t.Error("a local Ollama needs no privacy note")
	}
}

func TestTestConnectionReportsInTheForm(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")
	b.post(id, "prefs/llm/edit?id=0", "")
	s.waitRegion(t, "modal", "Add LLM")

	// An invalid form is reported without a request.
	b.postForm(id, "prefs/llm/test", formValues("", "ollama", "", "http://localhost:11434", ""))
	s.waitRegion(t, "modal", "Model is required.")

	// A server that is not there: the failure comes back to the form.
	dead := "http://127.0.0.1:" + uitoa(uint(freePort(t)))
	b.postForm(id, "prefs/llm/test", formValues("", "openai-compatible", "m", dead, ""))
	s.waitRegion(t, "modal", `class="error" role="status">Failed:`)
}

func TestGeneralSettingsApplyAtOnce(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")
	b.post(id, "prefs/tab?tab=general", "")
	s.waitRegion(t, "modal", "Auto-check delay", `value="3"`, `value="13"`)

	b.postForm(id, "prefs/general", url.Values{"auto_check": {"1"}, "delay_s": {"7"}, "font_size": {"18"}})
	s.waitSignal(t, "_fs", float64(18))
	s.waitRegion(t, "toolbar", `id="auto" type="checkbox" checked`)
	if on, _ := e.st.AutoCheck(t.Context()); !on {
		t.Error("auto-check is stored")
	}
	if n, _ := e.st.AutoCheckDelaySeconds(t.Context()); n != 7 {
		t.Errorf("delay = %d", n)
	}
	if n, _ := e.st.FontSize(t.Context()); n != 18 {
		t.Errorf("font size = %d", n)
	}

	for _, bad := range []url.Values{
		{"delay_s": {"0"}, "font_size": {"13"}},
		{"delay_s": {"3"}, "font_size": {"100"}},
		{"delay_s": {"x"}, "font_size": {"13"}},
	} {
		b.postForm(id, "prefs/general", bad)
		s.waitRegion(t, "modal", "must be a whole number from")
		if n, _ := e.st.FontSize(t.Context()); n != 18 {
			t.Errorf("a refused value was stored: %d", n)
		}
		b.post(id, "prefs/tab?tab=general", "")
		s.waitRegion(t, "modal", `value="18"`)
	}
}

func TestStatisticsDialog(t *testing.T) {
	fake := llmtest.New(twoIssues())
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	eventually(t, "mistakes", func() bool { r, _ := e.stats.TypeStats(t.Context()); return len(r) == 2 })

	b.post(id, "stats/open", sampleText)
	tbl := s.waitRegion(t, "modal", "Statistics", "2 mistakes in all time.", "Verb form", "Spelling", "Grammar", "Vocabulary")
	if !strings.Contains(tbl, "Total") || !strings.Contains(tbl, "Last 7 days") || !strings.Contains(tbl, "Last seen") {
		t.Errorf("the table lacks columns: %q", tbl)
	}

	// Category filter and period selector.
	b.postForm(id, "stats/filter", url.Values{"category": {"grammar"}, "period": {"7"}})
	f := s.waitRegion(t, "modal", "1 mistake in the last 7 days.")
	if strings.Contains(f, "Spelling") && strings.Contains(f, `data-type="spelling"`) {
		t.Errorf("the filter must hide the other category: %q", f)
	}

	// Sorting: a click on a header sorts, a second click reverses.
	b.postForm(id, "stats/filter", url.Values{"category": {""}, "period": {"all"}})
	s.waitRegion(t, "modal", "2 mistakes in all time.")
	b.post(id, "stats/sort?col=0", sampleText)
	asc := s.waitRegion(t, "modal", "Error type ▲")
	if strings.Index(asc, `data-type="spelling"`) > strings.Index(asc, `data-type="verb_form"`) {
		t.Errorf("by name, Spelling comes before Verb form: %q", asc)
	}
	b.post(id, "stats/sort?col=0", sampleText)
	s.waitRegion(t, "modal", "Error type ▼")

	// A row shows the user's own wrong text.
	b.post(id, "stats/select?type=verb_form", sampleText)
	s.waitRegion(t, "modal", "Your most recent examples", "He go")

	// Token usage tab.
	b.post(id, "stats/tab?tab=usage", sampleText)
	s.waitRegion(t, "modal", "local", "120 in / 30 out", "Reset token usage")
	b.post(id, "stats/reset?which=usage", sampleText)
	s.waitRegion(t, "modal", "No requests recorded yet.")
	if rows, _ := e.stats.UsageTotals(t.Context(), store.AllTime); len(rows) != 0 {
		t.Errorf("usage was not reset: %v", rows)
	}
	b.post(id, "stats/tab?tab=mistakes", sampleText)
	s.waitRegion(t, "modal", "2 mistakes in all time.")

	// Reset clears the counts, the badges and the header.
	s.waitRegion(t, "issues", `class="badge"`)
	b.post(id, "stats/reset?which=mistakes", sampleText)
	s.waitRegion(t, "modal", "No mistakes recorded yet.")
	if strings.Contains(s.waitRegion(t, "issues", "Issues (2)"), `class="badge"`) {
		t.Error("the badges go with the statistics")
	}
	if rows, _ := e.stats.TypeStats(t.Context()); len(rows) != 0 {
		t.Errorf("mistakes were not reset: %v", rows)
	}
	b.post(id, "modal/close", sampleText)
	s.waitRegion(t, "modal", " hidden")
}

func TestStatisticsWithNothingRecorded(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "stats/open", "")
	s.waitRegion(t, "modal", "No mistakes recorded yet.", "No mistakes recorded for all time.")
	b.post(id, "stats/tab?tab=usage", "")
	s.waitRegion(t, "modal", "No requests recorded yet.")
}

func TestDialogsSurviveAReconnect(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "library/open", "")
	s.waitRegion(t, "modal", "Library")
	s.cancel()

	s2 := b.stream(id)
	s2.waitRegion(t, "modal", "Library")
	s2.waitSignal(t, "_modified", false) // the signals come with the elements
	s2.quiet(t)
	if _, ok := s2.signals["text"]; ok {
		t.Error("a browser that comes back keeps its own text: the stream must not send it again")
	}
}

func TestLLMRequestSettingsForm(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "prefs/open", "")

	// A new form starts with the default timeout and one request at a time.
	b.post(id, "prefs/llm/edit?id=0", "")
	form := s.waitRegion(t, "modal", "Add LLM", `name="timeout"`, `name="max_parallel"`)
	if !strings.Contains(form, `name="max_parallel" min="1" max="16" step="1" value="1"`) {
		t.Errorf("parallel requests start at 1: %q", form)
	}
	if !strings.Contains(form, `name="no_thinking" value="1" checked>`) {
		t.Errorf("No thinking starts ticked: %q", form)
	}

	// Bad values stay in the form with a message and are not stored.
	tests := []struct {
		name, timeout, parallel, want string
	}{
		{"timeout too short", "5", "1", "Timeout must be empty or between 10 and 3600 seconds."},
		{"timeout not a number", "abc", "1", "Timeout must be a number of seconds"},
		{"too many parallel", "", "17", "Parallel requests must be between 1 and 16."},
		{"parallel not a number", "", "x", "Parallel requests must be a whole number."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := formValues("fast", "ollama", "m", "http://localhost:11434", "")
			v.Set("timeout", tt.timeout)
			v.Set("max_parallel", tt.parallel)
			b.postForm(id, "prefs/llm/save", v)
			s.waitRegion(t, "modal", tt.want, "Add LLM")
		})
	}

	// Good values are stored and shown again when the LLM is edited.
	v := formValues("fast", "ollama", "m", "http://localhost:11434", "")
	v.Set("timeout", "240")
	v.Set("max_parallel", "4")
	b.postForm(id, "prefs/llm/save", v)
	s.waitRegion(t, "modal", ">fast<")
	cfgs, _ := e.st.ListLLMConfigs(t.Context())
	var fast store.LLMConfig
	for _, c := range cfgs {
		if c.Name == "fast" {
			fast = c
		}
	}
	if fast.TimeoutSeconds != 240 || fast.MaxParallel != 4 {
		t.Fatalf("stored = %+v, want timeout 240 and parallel 4", fast)
	}
	b.post(id, "prefs/llm/edit?id="+uitoa(fast.ID), "")
	s.waitRegion(t, "modal", "Edit LLM", `name="timeout" min="10" max="3600" step="1" value="240"`, `max="16" step="1" value="4"`)

	// No thinking is stored, shown again, and dropped for a provider it does nothing for.
	v.Set("no_thinking", "1")
	b.postForm(id, "prefs/llm/save", v)
	s.waitRegion(t, "modal", ">fast<")
	if got, _ := e.st.GetLLMConfig(t.Context(), fast.ID); !got.NoThinking {
		t.Errorf("No thinking was not stored: %+v", got)
	}
	b.post(id, "prefs/llm/edit?id="+uitoa(fast.ID), "")
	s.waitRegion(t, "modal", "Edit LLM", `name="no_thinking" value="1" checked>`)
	anthropic := formValues("fast", "anthropic", "claude-haiku-5-5", "", secretKey)
	anthropic.Set("no_thinking", "1")
	b.postForm(id, "prefs/llm/save", anthropic)
	s.waitRegion(t, "modal", ">fast<")
	if got, _ := e.st.GetLLMConfig(t.Context(), fast.ID); got.NoThinking {
		t.Errorf("No thinking does nothing for Anthropic and must not be stored: %+v", got)
	}
	v.Del("no_thinking")
	b.postForm(id, "prefs/llm/save", v)
	s.waitRegion(t, "modal", ">fast<")

	// An empty timeout goes back to the default.
	v.Set("timeout", "")
	b.postForm(id, "prefs/llm/save", v)
	s.waitRegion(t, "modal", ">fast<")
	if got, _ := e.st.GetLLMConfig(t.Context(), fast.ID); got.TimeoutSeconds != 0 {
		t.Errorf("an empty timeout is the default, got %d", got.TimeoutSeconds)
	}
}
