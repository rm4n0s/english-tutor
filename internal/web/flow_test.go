package web

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

func twoIssues() llmtest.Reply {
	return llmtest.Reply{Text: issuesReply("He go", "verb_form", "She have", "spelling"), Info: tokenInfo}
}

func TestPageHasEverythingTheDesktopWindowHas(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	_, page := b.open()
	for _, want := range []string{
		`id="toolbar"`, `id="llm"`, `id="btn-check"`, `id="auto"`, "Statistics", "Preferences",
		">New<", ">Open<", ">Save<", ">Save As<", ">Save to library<", ">Library<",
		"Legend:", "~~ Grammar", "-- Vocabulary", ".. Syntax",
		`id="text"`, `id="underlay"`, `id="issues"`, "Issues (0)", "Press Check to find errors",
		`id="explain"`, "Select an error in the list", `id="statusbar"`, "Ready", "0 words",
		`id="banner"`, `id="modal"`, `/static/datastar.js`, `@get('events')`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not contain %q", want)
		}
	}
	if !strings.Contains(page, `<option value="`) || !strings.Contains(page, ">local</option>") {
		t.Error("the LLM selector must list the configured LLMs")
	}
}

func TestStreamSendsTheWholePageThenOnlyWhatChanged(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)

	for _, r := range []string{"toolbar", "banner", "underlay", "issues", "explain", "status", "modal"} {
		s.waitRegion(t, r, "")
	}
	s.waitSignal(t, "_title", "Untitled — English Tutor")
	s.waitSignal(t, "_modified", false)
	s.quiet(t)
	n := len(s.all)

	// Typing only touches the layer under the editor, the status and the title.
	if code := b.post(id, "text", "Hello there"); code != 204 {
		t.Fatalf("text = %d", code)
	}
	s.waitRegion(t, "status", "2 words")
	s.waitSignal(t, "_modified", true)
	s.waitSignal(t, "_title", "Untitled* — English Tutor")
	s.quiet(t)
	for _, ev := range s.all[n:] {
		if strings.Contains(ev.Data, `id="toolbar"`) || strings.Contains(ev.Data, `id="explain"`) || strings.Contains(ev.Data, `id="issues"`) {
			t.Errorf("typing must not resend the toolbar, the list or the explanation: %q", ev.Data)
		}
	}
}

func TestCheckUnderlinesListsAndRecords(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesReply("He go", "verb_form", "She have", "spelling"), Info: tokenInfo, Delay: 150 * time.Millisecond})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)

	if code := b.post(id, "check", sampleText); code != 204 {
		t.Fatalf("check = %d", code)
	}
	s.waitRegion(t, "toolbar", ">Cancel<")
	s.waitRegion(t, "status", "Checking...")
	s.waitRegion(t, "issues", "Checking...")

	under := s.waitRegion(t, "underlay", `class="u u-grammar"`)
	for _, want := range []string{
		"<span class=\"u u-grammar\" data-tip=\"Verb form · Grammar\nmsg for He go\">He go</span>",
		"<span class=\"u u-vocabulary\" data-tip=\"Spelling · Vocabulary\nmsg for She have\">She have</span>",
		" home. ", " a cat.",
	} {
		if !strings.Contains(under, want) {
			t.Errorf("the underlines lack %q in %q", want, under)
		}
	}
	list := s.waitRegion(t, "issues", "Issues (2)", `data-id="1"`, `data-id="2"`, "He go", "She have")
	if !strings.Contains(list, `class="mark cat-grammar">G<`) || !strings.Contains(list, `class="mark cat-vocabulary">V<`) {
		t.Errorf("rows carry a letter for the category: %q", list)
	}
	s.waitRegion(t, "status", "2 issues", "last: 120 in / 30 out")
	s.waitRegion(t, "toolbar", ">Check<")

	eventually(t, "the mistakes to be recorded", func() bool {
		rows, err := e.stats.TypeStats(t.Context())
		return err == nil && len(rows) == 2
	})
	// The badges of the list appear once the counts are read.
	s.waitRegion(t, "issues", `class="badge">x1</span>`)
}

func TestAutoCheckDoesNotRecord(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	if err := e.st.SetAutoCheckDelaySeconds(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetAutoCheck(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "text", sampleText)
	s.waitRegion(t, "underlay", `class="u u-grammar"`)
	list := s.waitRegion(t, "issues", "Issues (2)")
	s.quiet(t)
	rows, err := e.stats.TypeStats(t.Context())
	if err != nil || len(rows) != 0 {
		t.Errorf("an auto-check must not be recorded: %v, %v", rows, err)
	}
	// Without a badge: the issue is not counted.
	if strings.Contains(list, `class="badge"`) || strings.Contains(s.regions["issues"], `class="badge"`) {
		t.Errorf("an auto-check issue has no count badge: %q", list)
	}
}

func TestAutoCheckWaitsForAPause(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	if err := e.st.SetAutoCheckDelaySeconds(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetAutoCheck(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	id, _ := b.open()
	b.post(id, "text", "He")
	time.Sleep(600 * time.Millisecond)
	b.post(id, "text", "He go") // typing again restarts the pause
	time.Sleep(600 * time.Millisecond)
	if n := len(e.fake.Calls()); n != 0 {
		t.Errorf("the LLM was called %d times before the pause ended", n)
	}
	eventually(t, "the auto-check", func() bool { return len(e.fake.Calls()) == 1 })
}

func TestSelectingAnIssueStreamsItsExplanation(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Chunks: []string{"## What is wrong\n", "Use **goes**."}, Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	s.waitRegion(t, "issues", `class="badge">x1</span>`)

	if code := b.post(id, "issue/select?id=1", sampleText); code != 204 {
		t.Fatalf("select = %d", code)
	}
	done := s.waitRegion(t, "explain", "<strong>goes</strong>", ">Regenerate<", "Not an error")
	if !strings.Contains(done, "<h4>What is wrong</h4>") {
		t.Errorf("the markdown must be rendered: %q", done)
	}
	s.waitRegion(t, "explain", "Verb form: you have made this mistake 1 time (1 in the last 7 days)")
	// The page is told to put the cursor at the start of the issue.
	s.waitSignal(t, "_goto", map[string]any{"seq": 1, "pos": 0})
	s.waitRegion(t, "underlay", `class="u u-grammar sel"`)
	s.waitRegion(t, "issues", `aria-selected="true"`)
}

func TestExplanationShowsLoadingAndCancel(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "late", Delay: 5 * time.Second})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")

	b.post(id, "issue/select?id=1", sampleText)
	s.waitRegion(t, "explain", "Explaining <strong>He go</strong>...", `class="spinner"`, ">Cancel<", "Not an error")
	b.post(id, "explain/cancel", sampleText)
	s.waitRegion(t, "explain", "Explanation cancelled.", ">Regenerate<")
}

func TestExplanationFailureOffersRetry(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Err: io.ErrUnexpectedEOF}, llmtest.Reply{Text: "Fixed.", Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	b.post(id, "issue/select?id=1", sampleText)
	s.waitRegion(t, "explain", `class="error"`, ">Retry<")
	b.post(id, "explain/regenerate", sampleText)
	s.waitRegion(t, "explain", "Fixed.", ">Regenerate<")
}

func TestCursorSelectsWithoutMovingTheCursor(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "Because.", Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	s.quiet(t)

	b.post(id, "cursor?pos=3", sampleText) // inside "He go"
	s.waitRegion(t, "underlay", `class="u u-grammar sel"`)
	s.waitRegion(t, "explain", "Because.")
	s.quiet(t)
	if _, moved := s.signals["_goto"]; moved {
		t.Errorf("a selection that came from the cursor must not move it: %v", s.signals["_goto"])
	}

	b.post(id, "cursor?pos=5000", sampleText)
	s.waitRegion(t, "explain", "Select an error in the list")
	s.waitRegion(t, "underlay", `class="u u-grammar"`)
}

func TestStepThroughIssues(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "Why.", Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")

	// With nothing selected, F7 goes to the first issue after the cursor.
	b.post(id, "issue/step?dir=next&pos=5", sampleText)
	s.waitSignal(t, "_goto", map[string]any{"seq": 1, "pos": 12}) // "She have" starts at offset 12
	b.post(id, "issue/step?dir=next&pos=0", sampleText)
	s.waitSignal(t, "_goto", map[string]any{"seq": 2, "pos": 0}) // wraps around
	b.post(id, "issue/step?dir=prev&pos=0", sampleText)
	s.waitSignal(t, "_goto", map[string]any{"seq": 3, "pos": 12})
}

func TestClickingTheSelectedRowAgainStillMovesThere(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "Why.", Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	b.post(id, "issue/select?id=2", sampleText)
	s.waitSignal(t, "_goto", map[string]any{"seq": 1, "pos": 12})
	b.post(id, "issue/select?id=2", sampleText)
	s.waitSignal(t, "_goto", map[string]any{"seq": 2, "pos": 12})
}

func TestNotAnErrorRemovesTheIssueAndItsCount(t *testing.T) {
	fake := llmtest.New(twoIssues(), llmtest.Reply{Text: "Why.", Info: tokenInfo})
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	eventually(t, "two mistakes recorded", func() bool {
		rows, _ := e.stats.TypeStats(t.Context())
		return len(rows) == 2
	})
	s.waitRegion(t, "issues", "Issues (2)")
	b.post(id, "issue/select?id=1", sampleText)
	s.waitRegion(t, "explain", "Not an error")
	b.post(id, "issue/not-an-error", sampleText)
	s.waitRegion(t, "issues", "Issues (1)")
	s.waitRegion(t, "explain", "Select an error in the list")
	eventually(t, "the count to go", func() bool {
		rows, _ := e.stats.TypeStats(t.Context())
		return len(rows) == 1
	})
}

func TestLegendHidesACategory(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	b.post(id, "legend?c=grammar", sampleText)
	s.waitRegion(t, "issues", "Issues (1 of 2 shown)")
	s.waitRegion(t, "toolbar", `aria-pressed="false" data-cat="grammar"`)
	if strings.Contains(s.waitRegion(t, "underlay", `class="u u-vocabulary"`), "u-grammar") {
		t.Error("the hidden category must lose its underlines")
	}
	b.post(id, "legend?c=grammar", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
}

func TestEditsMoveOrRemoveUnderlines(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	text := "Hi. " + sampleText
	b.post(id, "check", text)
	s.waitRegion(t, "issues", "Issues (2)")

	// Typing before both issues moves them.
	b.post(id, "text", "Oh. "+text)
	s.waitRegion(t, "underlay", `Oh. Hi. <span class="u u-grammar"`)
	// Typing inside the first one removes it.
	b.post(id, "text", "Oh. Hi. He gXo home. She have a cat.")
	s.waitRegion(t, "issues", "Issues (1)")
	if strings.Contains(s.waitRegion(t, "underlay", "u-vocabulary"), "u-grammar") {
		t.Error("an edit inside an issue removes its underline")
	}
}

func TestCancelAndFailure(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: issuesReply(), Delay: 5 * time.Second},
		llmtest.Reply{Err: io.ErrUnexpectedEOF},
	)
	e := newEnv(t, fake, Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)

	b.post(id, "check", sampleText)
	s.waitRegion(t, "toolbar", ">Cancel<")
	b.post(id, "cancel", sampleText)
	s.waitRegion(t, "status", "Check cancelled")
	s.waitRegion(t, "toolbar", ">Check<")
	if strings.Contains(s.regions["banner"], "alert\"> ") || !strings.Contains(s.regions["banner"], " hidden") {
		t.Errorf("cancelling is not an error: %q", s.regions["banner"])
	}

	b.post(id, "check", sampleText)
	s.waitRegion(t, "status", "Check failed")
	banner := s.waitRegion(t, "banner", `class="msg">`)
	if strings.Contains(banner, " hidden") {
		t.Errorf("a failed check shows its message in the banner: %q", banner)
	}
	b.post(id, "banner/dismiss", sampleText)
	s.waitRegion(t, "banner", ` hidden`)
}

func TestNothingToCheck(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", "   ")
	s.waitRegion(t, "status", "Nothing to check yet. Write some text first.")
	if n := len(e.fake.Calls()); n != 0 {
		t.Errorf("the LLM was called %d times for an empty text", n)
	}
}

func TestSelectingAnotherLLMIsRemembered(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	id2 := addLLM(t, e, "second")
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	s.waitRegion(t, "toolbar", ">second</option>")
	b.post(id, "llm?id="+uitoa(id2), "")
	s.waitRegion(t, "toolbar", `value="`+uitoa(id2)+`" selected`)
	if got, _ := e.st.SelectedLLMID(t.Context()); got != id2 {
		t.Errorf("the desktop's remembered selection = %d, want %d", got, id2)
	}
	select {
	case <-e.changed:
	case <-time.After(time.Second):
		t.Error("the desktop window must be told")
	}
}

func TestAutoCheckBoxIsASetting(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "auto?on=true", "")
	s.waitRegion(t, "toolbar", `id="auto" type="checkbox" checked`)
	if on, _ := e.st.AutoCheck(t.Context()); !on {
		t.Error("the Auto-check box is stored, like the desktop's")
	}
	b.post(id, "auto?on=false", "")
	eventually(t, "the setting to be stored", func() bool {
		on, _ := e.st.AutoCheck(t.Context())
		return !on
	})
}

func TestNoLLMDisablesCheckAndSaysSo(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	if err := e.st.DeleteLLMConfig(t.Context(), e.llmID); err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	_, page := b.open()
	if !strings.Contains(page, "No LLM is configured yet.") {
		t.Error("the panel must say that no LLM is configured")
	}
	if !strings.Contains(page, `data-on:click="@post('check')" disabled`) {
		t.Error("Check is disabled without an LLM")
	}
	if !strings.Contains(page, `<select id="llm" data-on:change="@post('llm?id=' + el.value)" disabled>`) {
		t.Error("the selector is disabled without an LLM")
	}
}
