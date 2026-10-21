package app

import (
	"context"
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// docShotsEnv names the folder TestDocScreenshots writes the pictures of
// docs/TUTORIAL.md to. Without it the test is skipped:
//
//	DOC_SCREENSHOTS_DIR=docs/images go test ./internal/app -run TestDocScreenshots
//
// The pictures come from the real window and dialogs with a fake LLM, so they
// show what the code draws. The tutorial's numbered callouts are added afterwards
// (docs/images/README.md says how).
const docShotsEnv = "DOC_SCREENSHOTS_DIR"

const docText = "Yesterday I go to the market with my sister. We buyed some fruits and vegetable for the dinner. " +
	"The seller was very friendly, but he give us a big discount. After that, we was tired so we took a taxi to home.\n\n" +
	"I like very much this city because the people are kind and the food is very delicious."

// docIssues are the mistakes of docText, in text order.
var docIssues = []struct{ text, typ, msg string }{
	{"I go", "verb_tense", "Yesterday points to the past, so use went."},
	{"buyed", "verb_form", "Buy is irregular: its past form is bought."},
	{"vegetable", "plural_singular", "Use the plural, vegetables, after some."},
	{"he give", "verb_tense", "The sentence is in the past: he gave."},
	{"we was", "subject_verb_agreement", "With we, use were."},
	{"to home", "preposition", "Home needs no preposition here: took a taxi home."},
	{"like very much this city", "word_order", "Put the object first: like this city very much."},
	{"very delicious", "collocation", "Delicious is already strong. Say really delicious, or just delicious."},
}

func docCheckReply() llmtest.Reply {
	parts := make([]string, len(docIssues))
	for i, is := range docIssues {
		parts[i] = fmt.Sprintf(`{"text":%q,"error_type":%q,"message":%q}`, is.text, is.typ, is.msg)
	}
	return llmtest.Reply{Text: `{"issues":[` + strings.Join(parts, ",") + `]}`, Info: map[string]any{"PromptTokens": 612, "CompletionTokens": 187}}
}

func docExplainReply() llmtest.Reply {
	return llmtest.Reply{Chunks: []string{
		"### What is wrong\nThe verb *buy* is irregular. Its past tense is **bought**, not *buyed*.\n\n",
		"### Why\nMost English verbs add *-ed* in the past (*walked*, *played*), but about two hundred common verbs change in other ways. *Buy* is one of them.\n\n",
		"### Better ways\n- We **bought** some fruit and vegetables for dinner.\n- We **picked up** some fruit and vegetables for dinner.\n\n",
		"### Remember\nLearn the three forms together: *buy - bought - bought*. The same pattern: *bring - brought*, *think - thought*.",
	}, Info: map[string]any{"PromptTokens": 240, "CompletionTokens": 118}}
}

func TestDocScreenshots(t *testing.T) {
	dir := os.Getenv(docShotsEnv)
	if dir == "" {
		t.Skip(docShotsEnv + " is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	h := newHarnessLLM(t, llmtest.New(docCheckReply(), docExplainReply()), "Llama 3.1 (Ollama)")
	use := func(f *llmtest.Fake) {
		onUI(func() { h.a.newModel = func(store.LLMConfig) (llms.Model, error) { return f, nil } })
	}
	shot := func(name string, widget func() *qt.QWidget) {
		t.Helper()
		onUI(func() {})
		time.Sleep(150 * time.Millisecond) // lists and dialogs fill themselves on the event loop
		onUI(func() {
			if !widget().Grab().Save(filepath.Join(dir, name)) {
				t.Errorf("cannot save %s", name)
			}
		})
	}
	window := func() *qt.QWidget { return h.a.win.Widget() }
	sentence := "After that, we was tired so we took a taxi to home."
	start := strings.Index(docText, sentence)
	end := start + len(sentence)

	onUI(func() {
		h.a.win.Widget().Resize(1100, 800)
		h.a.win.SetFontSize(12)
		h.a.win.SetSplitterSizes([]int{770, 330, 320, 410}) // room for the whole issues list
		h.a.win.Show()
	})
	h.setText(docText)
	onUI(func() { h.a.win.MoveCursorTo(0) })
	shot("01-write.png", window)

	h.checkButton()
	h.waitIdle()
	h.waitFor("the mistakes to be recorded", func() bool { return h.events() >= len(docIssues) })
	h.waitFor("the badges", func() bool { return len(h.a.typeCounts) > 0 })
	onUI(func() { h.a.win.MoveCursorTo(len(docText)) }) // nothing selected: the cursor is in no issue
	shot("02-checked.png", window)

	h.selectIssue(2) // "buyed"
	h.waitPanel(view.ExplainReady)
	h.waitFor("the header counts", func() bool { return strings.Contains(h.a.win.ExplanationHeader(), "you have made") })
	shot("03-explanation.png", window)

	onUI(func() {
		h.a.win.SelectRange(start, end)
		if !h.a.win.EditorMenuPicture(filepath.Join(dir, "04-context-menu.png"), "Check") {
			t.Error("cannot save the menu")
		}
	})

	use(llmtest.New(llmtest.Reply{Text: issuesReply(nil), Delay: time.Minute}))
	h.checkSelection(start, end)
	shot("05-checking.png", window)
	h.checkButton() // Cancel
	h.waitIdle()

	use(llmtest.New(altReply(
		"Afterwards, we were tired, so we took a taxi home.",
		"We were tired by then, so we caught a taxi home.",
		"Being tired, we took a taxi home.",
		"After that, we were so tired that we took a taxi home.",
	)))
	h.askAlternatives(start, end)
	h.waitAlternatives()
	shot("06-alternatives.png", func() *qt.QWidget { return h.a.altDlg.Dialog().QWidget })
	onUI(func() { h.a.closeAlternatives() })

	// Other mistakes the learner made in earlier texts, for the statistics.
	earlier := []struct {
		typ      string
		snippets []string
	}{
		{"subject_verb_agreement", []string{"she don't like", "the children is", "my brother have", "he go", "it make"}},
		{"article", []string{"I bought car", "She is teacher", "in the Monday", "have a advice"}},
		{"preposition", []string{"depends of", "arrive to the station", "interested on"}},
		{"verb_tense", []string{"I am here since May", "she told me she will come", "when I will arrive"}},
		{"spelling", []string{"recieve", "definately"}},
		{"collocation", []string{"make a photo"}},
		{"word_order", []string{"always I drink tea"}},
	}
	for _, e := range earlier {
		var ms []store.Mistake
		for i, snippet := range e.snippets {
			ms = append(ms, store.Mistake{
				ErrorType: e.typ, Category: string(analysis.Resolve(e.typ).Category),
				Snippet: snippet, Fingerprint: fmt.Sprintf("%s-%d", e.typ, i),
			})
		}
		if _, err := h.a.stats.Record(context.Background(), "file:/home/me/essays/week1.txt", "Llama 3.1 (Ollama)", ms); err != nil {
			t.Fatal(err)
		}
	}
	var sdlg *ui.StatisticsDialog
	onUI(func() {
		sdlg = ui.NewStatisticsDialog(h.a.ctx, h.a.win.Widget(), h.a.stats, ui.StatisticsHooks{})
		sdlg.Dialog().Resize(760, 620)
		sdlg.Dialog().Show()
		sdlg.SelectRow(0)
	})
	shot("07-statistics.png", func() *qt.QWidget { return sdlg.Dialog().QWidget })
	onUI(func() { sdlg.Dialog().Delete() })

	var form *ui.LLMForm
	onUI(func() {
		form = ui.NewLLMForm(h.a.ctx, h.a.win.Widget(), preferences{store: h.a.store}, store.LLMConfig{
			Name: "Llama 3.1 (Ollama)", Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2, NoThinking: true,
		}, func(*store.LLMConfig) error { return nil })
		form.Dialog().Show()
	})
	shot("09-llm-form.png", func() *qt.QWidget { return form.Dialog().QWidget })
	onUI(func() { form.Dialog().Delete() })

	var pdlg *ui.PreferencesDialog
	onUI(func() {
		pdlg = ui.NewPreferencesDialog(h.a.ctx, h.a.win.Widget(), preferences{store: h.a.store}, ui.PreferencesHooks{
			Changed: func() {}, Failed: func(error) {}, WebToggle: func() {}, WebChanged: func() {},
			WebState: func() (bool, string) { return false, "" },
		})
		pdlg.Dialog().Show()
	})
	shot("08-preferences.png", func() *qt.QWidget { return pdlg.Dialog().QWidget })
	onUI(func() { pdlg.Dialog().Delete() })
}
