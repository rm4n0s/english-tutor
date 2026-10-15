package ui

import (
	"slices"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/uitest"
)

// ui runs f on the Qt thread with a fresh window.
func withWindow(t *testing.T, f func(w *MainWindow)) {
	t.Helper()
	uitest.OnUI(func() {
		w := NewMainWindow()
		defer w.win.Delete()
		f(w)
	})
}

func choices() []LLMChoice {
	return []LLMChoice{{ID: 3, Name: "local"}, {ID: 8, Name: "claude"}}
}

func TestNewWindowStartsEmptyAndDisabled(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		if w.llmCombo.IsEnabled() || w.checkBtn.IsEnabled() {
			t.Error("with no LLM the selector and the Check button must be disabled")
		}
		if got := w.checkBtn.Text(); got != "Check" {
			t.Errorf("check button = %q", got)
		}
		if w.AutoCheck() {
			t.Error("auto-check must be off by default")
		}
		if got := w.explain.ToPlainText(); !strings.Contains(got, "No LLM is configured yet") || !strings.Contains(got, "Preferences") {
			t.Errorf("explanation panel = %q", got)
		}
		if got := w.issuesHeader.Text(); got != "Issues (0)" {
			t.Errorf("issues header = %q", got)
		}
		if w.issuesList.Count() != 1 || w.issuesList.Item(0).Text() != "Press Check to find errors" {
			t.Error("the list should show the single empty-state line")
		}
		if got := w.stateLabel.Text(); got != "Ready" {
			t.Errorf("state = %q", got)
		}
		if got := w.wordsLabel.Text(); got != "0 words" {
			t.Errorf("words = %q", got)
		}
		if got := w.tokensLabel.Text(); got != "last: 0 in / 0 out · session: 0 in / 0 out" {
			t.Errorf("tokens = %q", got)
		}
		if w.SelectedLLMID() != 0 {
			t.Error("no LLM should be selected")
		}
	})
}

func TestLegendOrder(t *testing.T) {
	if !slices.Equal(legendOrder, []analysis.Category{analysis.Grammar, analysis.Vocabulary, analysis.Syntax}) {
		t.Errorf("legend order = %v", legendOrder)
	}
	// Every category has an entry, so adding one to the taxonomy cannot leave
	// it out of the legend.
	for _, c := range analysis.Categories() {
		if !slices.Contains(legendOrder, c) {
			t.Errorf("category %s is missing from the legend", c)
		}
	}
}

func TestLayout(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		if w.hSplit.Orientation() != qt.Horizontal || w.vSplit.Orientation() != qt.Vertical {
			t.Error("the editor and right column sit in a horizontal splitter; issues and explanation in a vertical one")
		}
		if w.hSplit.Count() != 2 || w.vSplit.Count() != 2 {
			t.Errorf("splitter children = %d, %d; want 2, 2", w.hSplit.Count(), w.vSplit.Count())
		}
		if w.editor.LineWrapMode() != qt.QPlainTextEdit__WidgetWidth {
			t.Error("the editor must wrap lines")
		}
		for _, c := range analysis.Categories() {
			b := w.legend[c]
			if b == nil || !b.IsChecked() || !strings.Contains(b.Text(), c.Label()) || !strings.Contains(b.Text(), CategorySample(c)) {
				t.Errorf("legend entry for %s is wrong", c)
				continue
			}
			if !strings.Contains(b.StyleSheet(), CategoryColor(c, false)) && !strings.Contains(b.StyleSheet(), CategoryColor(c, true)) {
				t.Errorf("legend entry for %s does not use its colour: %s", c, b.StyleSheet())
			}
		}
		if w.explain.OpenLinks() || w.explain.OpenExternalLinks() {
			t.Error("Qt must never open links from the explanation itself")
		}
	})
}

func TestSetLLMs(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetLLMs(choices(), 8)
		if w.SelectedLLMID() != 8 || w.llmCombo.CurrentText() != "claude" {
			t.Errorf("selected = %d %q, want 8 claude", w.SelectedLLMID(), w.llmCombo.CurrentText())
		}
		if !w.llmCombo.IsEnabled() || !w.checkBtn.IsEnabled() {
			t.Error("with LLMs the selector and Check must be enabled")
		}
		if got := w.explain.ToPlainText(); !strings.Contains(got, "Select an error") {
			t.Errorf("hint = %q", got)
		}

		w.SetLLMs(choices(), 999)
		if w.SelectedLLMID() != 3 {
			t.Errorf("an unknown ID should select the first entry, got %d", w.SelectedLLMID())
		}

		w.SetLLMs(nil, 3)
		if w.llmCombo.IsEnabled() || w.checkBtn.IsEnabled() || w.SelectedLLMID() != 0 {
			t.Error("deleting every LLM must disable the selector and Check")
		}
		if !strings.Contains(w.explain.ToPlainText(), "No LLM is configured yet") {
			t.Error("the no-LLM message should return")
		}
	})
}

func TestSetLLMsDoesNotOverwriteAnExplanation(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetExplanation(ExplainReady, "Verb form", "### What is wrong\nUse goes.")
		w.SetLLMs(choices(), 3)
		if got := w.explain.ToPlainText(); !strings.Contains(got, "Use goes.") {
			t.Errorf("panel = %q: changing the LLM list must not replace an explanation", got)
		}
	})
}

func TestLLMSelectedHandler(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var got []uint
		w.SetHandlers(Handlers{LLMSelected: func(id uint) { got = append(got, id) }})
		w.SetLLMs(choices(), 3)
		w.llmCombo.SetCurrentIndex(1)
		w.llmCombo.Activated(1)
		if !slices.Equal(got, []uint{8}) {
			t.Errorf("handler calls = %v, want [8]", got)
		}
		// Filling the list from code is not a user choice.
		w.SetLLMs(choices(), 8)
		if !slices.Equal(got, []uint{8}) {
			t.Errorf("SetLLMs must not call the handler, got %v", got)
		}
	})
}

func TestCheckButtonBecomesCancel(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var calls []string
		w.SetHandlers(Handlers{
			Check:  func() { calls = append(calls, "check") },
			Cancel: func() { calls = append(calls, "cancel") },
		})
		w.SetLLMs(choices(), 3)

		w.checkBtn.Click()
		w.SetChecking(true)
		if w.checkBtn.Text() != "Cancel" || !w.checkBtn.IsEnabled() {
			t.Errorf("while checking: %q enabled=%v", w.checkBtn.Text(), w.checkBtn.IsEnabled())
		}
		w.checkBtn.Click()
		w.SetChecking(false)
		if w.checkBtn.Text() != "Check" {
			t.Errorf("after checking: %q", w.checkBtn.Text())
		}
		w.checkBtn.Click()
		if !slices.Equal(calls, []string{"check", "cancel", "check"}) {
			t.Errorf("calls = %v", calls)
		}
	})
}

func TestCheckStaysDisabledWithoutLLMEvenAfterChecking(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetChecking(true)
		w.SetChecking(false)
		if w.checkBtn.IsEnabled() {
			t.Error("Check must stay disabled when no LLM is configured")
		}
	})
}

func TestToolbarHandlers(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var calls []string
		rec := func(name string) func() { return func() { calls = append(calls, name) } }
		w.SetHandlers(Handlers{
			Statistics: rec("stats"), Preferences: rec("prefs"),
			New: rec("new"), Open: rec("open"), Save: rec("save"), SaveAs: rec("saveas"), SaveToLibrary: rec("savelibrary"), Library: rec("library"),
		})
		w.statsBtn.Click()
		w.prefsBtn.Click()
		for _, b := range w.fileBtns {
			b.Click()
		}
		want := []string{"stats", "prefs", "new", "open", "save", "saveas", "savelibrary", "library"}
		if !slices.Equal(calls, want) {
			t.Errorf("calls = %v, want %v", calls, want)
		}
		var names []string
		for _, b := range w.fileBtns {
			names = append(names, b.Text())
		}
		if !slices.Equal(names, []string{"New", "Open", "Save", "Save As", "Save to library", "Library"}) {
			t.Errorf("file buttons = %v", names)
		}
	})
}

func TestUnsetHandlersAreHarmless(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetLLMs(choices(), 3)
		w.checkBtn.Click()
		w.statsBtn.Click()
		w.prefsBtn.Click()
		w.autoCheck.Click()
		w.legend[analysis.Grammar].Click()
		w.SetText("hello there")
		w.llmCombo.Activated(0)
		w.explain.AnchorClicked(qt.NewQUrl3("preferences:"))
	})
}

func TestAutoCheck(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var got []bool
		w.SetHandlers(Handlers{AutoCheckChanged: func(on bool) { got = append(got, on) }})
		w.SetAutoCheck(true) // from settings: not a user action
		if !w.AutoCheck() || len(got) != 0 {
			t.Errorf("SetAutoCheck: checked=%v calls=%v", w.AutoCheck(), got)
		}
		w.autoCheck.Click()
		w.autoCheck.Click()
		if !slices.Equal(got, []bool{false, true}) {
			t.Errorf("calls = %v, want [false true]", got)
		}
	})
}

func TestLegendToggles(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		type toggle struct {
			c       analysis.Category
			visible bool
		}
		var got []toggle
		w.SetHandlers(Handlers{CategoryToggled: func(c analysis.Category, v bool) { got = append(got, toggle{c, v}) }})
		w.legend[analysis.Vocabulary].Click()
		w.legend[analysis.Vocabulary].Click()
		w.legend[analysis.Syntax].Click()
		want := []toggle{{analysis.Vocabulary, false}, {analysis.Vocabulary, true}, {analysis.Syntax, false}}
		if !slices.Equal(got, want) {
			t.Errorf("toggles = %v, want %v", got, want)
		}
	})
}

func TestTextChangesUpdateTheWordCount(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		changes := 0
		w.SetHandlers(Handlers{TextChanged: func() { changes++ }})
		tests := []struct{ text, want string }{
			{"one", "1 word"},
			{"one two  three\nfour", "4 words"},
			{"", "0 words"},
		}
		for _, tt := range tests {
			w.SetText(tt.text)
			if got := w.wordsLabel.Text(); got != tt.want {
				t.Errorf("after %q: %q, want %q", tt.text, got, tt.want)
			}
			if w.Text() != tt.text {
				t.Errorf("Text() = %q", w.Text())
			}
		}
		if changes < 3 {
			t.Errorf("TextChanged called %d times, want at least 3", changes)
		}
	})
}

func TestStatusAndTokens(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetStatus("3 issues | checking...")
		w.SetTokens(llm.Usage{InputTokens: 812, OutputTokens: 164}, llm.Usage{InputTokens: 5234, OutputTokens: 1100})
		w.SetIssues("Issues (0)", nil, 0, "No errors found")
		if w.stateLabel.Text() != "3 issues | checking..." {
			t.Errorf("state = %q", w.stateLabel.Text())
		}
		if got := w.tokensLabel.Text(); got != "last: 812 in / 164 out · session: 5.2k in / 1.1k out" {
			t.Errorf("tokens = %q", got)
		}
		if w.issuesList.Count() != 1 || w.issuesList.Item(0).Text() != "No errors found" {
			t.Error("SetIssues must replace the previous line")
		}
	})
}

func TestFontSize(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetFontSize(17)
		if got := w.editor.Font().PointSize(); got != 17 {
			t.Errorf("point size = %d", got)
		}
		w.SetFontSize(0) // ignored
		if got := w.editor.Font().PointSize(); got != 17 {
			t.Errorf("invalid size changed the font to %d", got)
		}
	})
}

func TestPreferencesLinkInTheNoLLMMessage(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		opened, asked := 0, 0
		w.SetLinkActions(func(string) bool { asked++; return false }, func(string) {})
		w.SetHandlers(Handlers{Preferences: func() { opened++ }})
		w.explain.AnchorClicked(qt.NewQUrl3("preferences:"))
		w.explain.AnchorClicked(qt.NewQUrl3("file:///etc/passwd"))
		if opened != 1 || asked != 0 {
			t.Errorf("Preferences opened %d times (want 1), %d confirmations (want 0): only the preferences: link is followed, and only in the hint", opened, asked)
		}
	})
}

func TestGeometryRoundTrip(t *testing.T) {
	var saved []byte
	withWindow(t, func(w *MainWindow) {
		w.Show()
		w.win.Resize(731, 523) // inside the 800x600 offscreen screen
		qt.QCoreApplication_ProcessEvents()
		saved = w.Geometry()
	})
	if len(saved) == 0 {
		t.Fatal("no geometry saved")
	}
	withWindow(t, func(w *MainWindow) {
		w.SetGeometry(saved)
		w.Show()
		qt.QCoreApplication_ProcessEvents()
		if got := w.win.Width(); got != 731 {
			t.Errorf("width = %d, want 731", got)
		}
		if got := w.win.Height(); got != 523 {
			t.Errorf("height = %d, want 523", got)
		}
		w.SetGeometry(nil)
		w.SetGeometry([]byte("garbage"))
		if w.win.Width() != 731 {
			t.Error("empty or damaged geometry must be ignored")
		}
	})
}

func TestSplitterSizesRoundTrip(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.win.Resize(1000, 700)
		w.Show()
		qt.QCoreApplication_ProcessEvents()
		w.SetSplitterSizes([]int{600, 300, 150, 350})
		qt.QCoreApplication_ProcessEvents()
		got := w.SplitterSizes()
		if len(got) != 4 {
			t.Fatalf("sizes = %v", got)
		}
		// The window chrome takes a few pixels, so compare proportions.
		if got[0] <= got[1]*3/2 || got[0] >= got[1]*5/2 {
			t.Errorf("horizontal sizes %v do not keep the 2:1 proportion", got[:2])
		}
		if got[2] >= got[3] {
			t.Errorf("vertical sizes %v should have the explanation larger", got[2:])
		}

		before := w.SplitterSizes()
		for _, bad := range [][]int{nil, {1, 2, 3}, {1, 2, 3, 4, 5}, {100, 0, 100, 100}, {100, -5, 100, 100}} {
			w.SetSplitterSizes(bad)
			if !slices.Equal(w.SplitterSizes(), before) {
				t.Errorf("SetSplitterSizes(%v) changed the sizes", bad)
			}
		}
	})
}

func TestCloseHandlers(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		closed := 0
		allow := false
		w.SetHandlers(Handlers{CanClose: func() bool { return allow }, Closed: func() { closed++ }})
		w.Show()
		w.Close()
		if !w.win.IsVisible() || closed != 0 {
			t.Errorf("CanClose=false: visible=%v closed=%d; the window must stay open", w.win.IsVisible(), closed)
		}
		allow = true
		w.Close()
		if w.win.IsVisible() || closed != 1 {
			t.Errorf("CanClose=true: visible=%v closed=%d", w.win.IsVisible(), closed)
		}
	})
}
