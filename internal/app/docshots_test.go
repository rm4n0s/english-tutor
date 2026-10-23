package app

import (
	"context"
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
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
		h.onUI(func() { h.a.newModel = func(store.LLMConfig) (llms.Model, error) { return f, nil } })
	}
	// shot saves what the window shows now, with the software renderer.
	shot := func(name string, animated ...bool) {
		t.Helper()
		for range 3 { // lists and dialogs fill themselves over a few frames
			h.tester.FlushPosted()
			if len(animated) > 0 {
				h.tester.Pump(100 * time.Millisecond) // a busy indicator never settles
			} else {
				h.tester.PumpAndSettle()
			}
		}
		img := h.tester.Screenshot()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}()
		if err := png.Encode(f, img); err != nil {
			t.Errorf("cannot save %s: %v", name, err)
		}
	}
	sentence := "After that, we was tired so we took a taxi to home."
	start := strings.Index(docText, sentence)
	end := start + len(sentence)

	h.tester.SetViewSize(ui.Size{Width: 1100, Height: 800})
	h.onUI(func() {
		fonts := store.DefaultFonts()
		fonts.Editor.Size = 12
		h.a.win.SetFonts(fonts)
		h.a.win.SetSplitterSizes([]int{770, 330, 320, 410}) // room for the whole issues list
		h.a.win.Show()
	})
	h.setText(docText)
	h.onUI(func() { h.a.win.MoveCursorTo(0) })
	shot("01-write.png")

	h.checkButton()
	h.waitIdle()
	h.waitFor("the mistakes to be recorded", func() bool { return h.events() >= len(docIssues) })
	h.waitFor("the badges", func() bool { return len(h.a.typeCounts) > 0 })
	h.onUI(func() { h.a.win.MoveCursorTo(len(docText)) }) // nothing selected: the cursor is in no issue
	shot("02-checked.png")

	h.selectIssue(2) // "buyed"
	h.waitPanel(view.ExplainReady)
	h.waitFor("the header counts", func() bool { return strings.Contains(h.a.win.ExplanationHeader(), "you have made") })
	shot("03-explanation.png")

	h.onUI(func() {
		h.a.win.SelectRange(start, end)
		if st, ok := h.a.win.Editor().FieldState(); ok {
			st.ShowToolbar()
		} else {
			t.Error("the editor is not on screen")
		}
	})
	shot("04-context-menu.png")
	h.onUI(func() { h.a.win.FocusEditor(); h.a.win.MoveCursorTo(0) })

	use(llmtest.New(llmtest.Reply{Text: issuesReply(nil), Delay: time.Minute}))
	h.checkSelection(start, end)
	shot("05-checking.png", true)
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
	shot("06-alternatives.png")
	h.onUI(func() { h.a.closeAlternatives() })

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
	var sdlg *gui.StatisticsDialog
	h.onUI(func() {
		sdlg = gui.NewStatisticsDialog(h.a.ctx, h.a.win, h.a.stats, gui.StatisticsHooks{})
		h.a.win.ShowDialog(sdlg)
	})
	h.tester.PumpAndSettle()
	h.onUI(func() { sdlg.SelectRow(0) })
	shot("07-statistics.png")
	h.onUI(sdlg.Close)
	h.tester.PumpAndSettle()

	h.onUI(func() {
		form := gui.NewLLMForm(h.a.ctx, h.a.rt, preferences{store: h.a.store}, store.LLMConfig{
			Name: "Llama 3.1 (Ollama)", Provider: "ollama", Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2, NoThinking: true,
		}, func(*store.LLMConfig) error { return nil }, nil)
		h.a.win.ShowDialog(form)
	})
	shot("09-llm-form.png")
	h.tester.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	h.tester.PumpAndSettle()

	h.onUI(func() {
		pdlg := gui.NewPreferencesDialog(h.a.ctx, h.a.win, preferences{store: h.a.store}, gui.PreferencesHooks{
			Changed: func() {}, Failed: func(error) {}, WebToggle: func() {}, WebChanged: func() {},
			WebState: func() (bool, string) { return false, "" },
		})
		h.a.win.ShowDialog(pdlg)
	})
	shot("08-preferences.png")
}
