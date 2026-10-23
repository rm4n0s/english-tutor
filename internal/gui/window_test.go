package gui

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
)

// pumpWindow shows a main window in a fresh app and view of the size of a window.
func pumpWindow(t *testing.T) (*gluttertest.Tester, *MainWindow) {
	t.Helper()
	tester := gluttertest.New(t)
	tester.SetViewSize(ui.Size{Width: 1100, Height: 700})
	w := NewMainWindow(NewRuntime(tester.Binding))
	t.Cleanup(w.Dispose)
	tester.PumpWidget(m.NewMaterialApp(m.MaterialAppOpts{Home: w.Widget()}))
	return tester, w
}

func choices() []LLMChoice { return []LLMChoice{{ID: 3, Name: "local"}, {ID: 8, Name: "claude"}} }

func tapText(t *testing.T, tester *gluttertest.Tester, text string) {
	t.Helper()
	f := gluttertest.ByText(text)
	if err := gluttertest.FindsOneWidget(tester, f); err != nil {
		t.Fatalf("tapping %q: %v", text, err)
	}
	tester.Tap(f)
	tester.Pump()
}

func TestNewWindowStartsEmptyAndDisabled(t *testing.T) {
	tester, w := pumpWindow(t)
	if w.CheckButtonEnabled() || w.SelectedLLMID() != 0 {
		t.Error("with no LLM the Check button must be disabled and nothing selected")
	}
	if w.CheckButtonText() != "Check" || w.AutoCheck() {
		t.Errorf("check = %q, auto-check = %v", w.CheckButtonText(), w.AutoCheck())
	}
	if got := w.ExplanationText(); !strings.Contains(got, "No LLM is configured yet") || !strings.Contains(got, "Preferences") {
		t.Errorf("explanation panel = %q", got)
	}
	if w.IssuesHeader() != "Issues (0)" || w.IssuesEmptyText() != "Press Check to find errors" {
		t.Errorf("issues: header %q, empty %q", w.IssuesHeader(), w.IssuesEmptyText())
	}
	if w.StatusText() != "Ready" || w.WordsText() != "0 words" || w.TokensText() != "last: 0 in / 0 out · session: 0 in / 0 out" {
		t.Errorf("status bar: %q | %q | %q", w.StatusText(), w.WordsText(), w.TokensText())
	}
	for _, text := range []string{"Check", "Statistics", "Preferences", "New", "Open", "Save", "Save As", "Save to library", "Library",
		"Clear", "Auto-check", "Legend:", "Issues (0)", "Press Check to find errors", "Ready", "0 words"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(text)); err != nil {
			t.Errorf("the window should show %q: %v", text, err)
		}
	}
	if w.Title() != "English Tutor" {
		t.Errorf("Title() = %q", w.Title())
	}
}

func TestLegendShowsTheCategoriesInOrder(t *testing.T) {
	tester, _ := pumpWindow(t)
	var xs []float64
	for _, c := range legendOrder {
		f := gluttertest.ByText(view.CategorySample(c) + " " + c.Label())
		if err := gluttertest.FindsOneWidget(tester, f); err != nil {
			t.Fatal(err)
		}
		xs = append(xs, tester.TopLeft(f).Dx)
	}
	if !slices.IsSorted(xs) {
		t.Errorf("legend entries are not in the order of legendOrder: %v", xs)
	}
}

func TestSetLLMs(t *testing.T) {
	_, w := pumpWindow(t)
	w.SetLLMs(choices(), 8)
	if w.SelectedLLMID() != 8 || !w.CheckButtonEnabled() {
		t.Errorf("selected %d, check enabled %v", w.SelectedLLMID(), w.CheckButtonEnabled())
	}
	w.SetLLMs(choices(), 99) // not in the list: the first one
	if w.SelectedLLMID() != 3 {
		t.Errorf("an unknown ID should select the first, got %d", w.SelectedLLMID())
	}
	if got := w.ExplanationText(); !strings.Contains(got, "Select an error") {
		t.Errorf("with an LLM the idle hint is the usual one, got %q", got)
	}
	w.SetLLMs(nil, 0)
	if w.CheckButtonEnabled() || w.SelectedLLMID() != 0 {
		t.Error("removing every LLM disables Check")
	}
}

func TestSetLLMsDoesNotOverwriteAnExplanation(t *testing.T) {
	_, w := pumpWindow(t)
	w.SetExplanation(view.ExplainReady, "Verb tense", "Use goes.")
	w.SetLLMs(choices(), 3)
	if w.ExplanationText() != "Use goes." {
		t.Errorf("the explanation was replaced by %q", w.ExplanationText())
	}
}

func TestChoosingAnLLMReportsIt(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []uint
	w.SetHandlers(Handlers{LLMSelected: func(id uint) { got = append(got, id) }})
	w.SetLLMs(choices(), 3)
	tester.Pump()
	tapText(t, tester, "local") // opens the list
	tapText(t, tester, "claude")
	if !slices.Equal(got, []uint{8}) || w.SelectedLLMID() != 8 {
		t.Errorf("reports %v, selected %d", got, w.SelectedLLMID())
	}
	w.SetLLMs(choices(), 3) // from code: reports nothing
	if len(got) != 1 {
		t.Errorf("selecting from code reported %v", got)
	}
}

func TestCheckButtonBecomesCancel(t *testing.T) {
	tester, w := pumpWindow(t)
	var calls []string
	w.SetHandlers(Handlers{Check: func() { calls = append(calls, "check") }, Cancel: func() { calls = append(calls, "cancel") }})
	w.SetLLMs(choices(), 3)
	tester.Pump()
	tapText(t, tester, "Check")
	w.SetChecking(true)
	tester.Pump()
	if w.CheckButtonText() != "Cancel" {
		t.Fatalf("button = %q", w.CheckButtonText())
	}
	tapText(t, tester, "Cancel")
	w.SetChecking(false)
	if !slices.Equal(calls, []string{"check", "cancel"}) || w.CheckButtonText() != "Check" {
		t.Errorf("calls %v, button %q", calls, w.CheckButtonText())
	}
}

func TestCheckStaysDisabledWithoutLLMEvenAfterChecking(t *testing.T) {
	_, w := pumpWindow(t)
	w.SetChecking(true)
	w.SetChecking(false)
	if w.CheckButtonEnabled() {
		t.Error("without an LLM Check must stay disabled")
	}
	var n int
	w.SetHandlers(Handlers{Check: func() { n++ }})
	w.CheckButtonClick()
	if n != 0 {
		t.Error("a disabled Check button must not report")
	}
}

func TestToolbarButtonsReportToTheirHandlers(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []string
	rec := func(name string) func() { return func() { got = append(got, name) } }
	w.SetHandlers(Handlers{Statistics: rec("stats"), Preferences: rec("prefs"), WebToggle: rec("web"),
		New: rec("new"), Open: rec("open"), Save: rec("save"), SaveAs: rec("saveas"),
		SaveToLibrary: rec("tolib"), Library: rec("lib")})
	for _, text := range []string{"Statistics", "Preferences", webStartText, "New", "Open", "Save", "Save As", "Save to library", "Library", "Clear"} {
		tapText(t, tester, text)
	}
	want := []string{"stats", "prefs", "web", "new", "open", "save", "saveas", "tolib", "lib", "new"} // Clear starts a new text
	if !slices.Equal(got, want) {
		t.Errorf("reports = %v, want %v", got, want)
	}
}

func TestUnsetHandlersAreHarmless(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetLLMs(choices(), 3)
	tester.Pump()
	for _, text := range []string{"Check", "Statistics", "Preferences", "New", "Save to library", "Clear"} {
		tapText(t, tester, text)
	}
	w.TypeIntoEditor("hello")
	w.LegendButtonClick(analysis.Grammar)
	w.ActivateShortcut(checkShortcut)
	w.Close()
}

func TestAutoCheckCheckboxReportsAndSetAutoCheckDoesNot(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []bool
	w.SetHandlers(Handlers{AutoCheckChanged: func(on bool) { got = append(got, on) }})
	tester.Tap(gluttertest.ByType[*m.Checkbox]())
	tester.Pump()
	if !w.AutoCheck() || !slices.Equal(got, []bool{true}) {
		t.Errorf("after a click: auto %v, reports %v", w.AutoCheck(), got)
	}
	w.SetAutoCheck(false)
	if w.AutoCheck() || len(got) != 1 {
		t.Errorf("SetAutoCheck must not report: auto %v, reports %v", w.AutoCheck(), got)
	}
}

func TestLegendToggles(t *testing.T) {
	tester, w := pumpWindow(t)
	type toggle struct {
		c  analysis.Category
		on bool
	}
	var got []toggle
	w.SetHandlers(Handlers{CategoryToggled: func(c analysis.Category, on bool) { got = append(got, toggle{c, on}) }})
	tapText(t, tester, view.CategorySample(analysis.Vocabulary)+" "+analysis.Vocabulary.Label())
	w.LegendButtonClick(analysis.Vocabulary)
	want := []toggle{{analysis.Vocabulary, false}, {analysis.Vocabulary, true}}
	if !slices.Equal(got, want) || !w.LegendChecked(analysis.Vocabulary) {
		t.Errorf("reports %v, want %v", got, want)
	}
}

func TestTextChangesUpdateTheWordCount(t *testing.T) {
	tester, w := pumpWindow(t)
	var changed int
	w.SetHandlers(Handlers{TextChanged: func() { changed++ }})
	w.TypeIntoEditor("one")
	if w.WordsText() != "1 word" {
		t.Errorf("one word: %q", w.WordsText())
	}
	w.SetText("one two  three")
	tester.Pump()
	if w.WordsText() != "3 words" || changed == 0 {
		t.Errorf("words %q, TextChanged calls %d", w.WordsText(), changed)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("3 words")); err != nil {
		t.Error(err)
	}
}

func TestStatusAndTokens(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetStatus("2 issues")
	w.SetTokens(llm.Usage{InputTokens: 120, OutputTokens: 30}, llm.Usage{InputTokens: 2400, OutputTokens: 600, Estimated: true})
	tester.Pump()
	if want := "last: 120 in / 30 out · session: ~2.4k in / ~600 out"; w.TokensText() != want {
		t.Errorf("tokens = %q, want %q", w.TokensText(), want)
	}
	for _, text := range []string{"2 issues", w.TokensText()} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(text)); err != nil {
			t.Error(err)
		}
	}
}

func TestSetFonts(t *testing.T) {
	chosen := store.Fonts{Editor: store.Font{Family: "Test Serif", Size: 18}, Buttons: store.Font{Size: 9}, Results: store.Font{Family: "Test Mono", Size: 12}}
	tests := []struct {
		name string
		set  []store.Fonts
		want store.Fonts
	}{
		{"defaults", nil, store.DefaultFonts()},
		{"every part", []store.Fonts{chosen}, chosen},
		{"a size below 1 keeps the font of the part", []store.Fonts{chosen, {Editor: store.Font{Family: "Other"}, Buttons: store.Font{Family: "B", Size: 10}, Results: chosen.Results}},
			store.Fonts{Editor: chosen.Editor, Buttons: store.Font{Family: "B", Size: 10}, Results: chosen.Results}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tester, w := pumpWindow(t)
			for _, f := range tt.set {
				w.SetFonts(f)
			}
			tester.Pump()
			if w.Fonts() != tt.want || w.editor.Font() != tt.want.Editor {
				t.Errorf("fonts %+v, editor %+v; want %+v", w.Fonts(), w.editor.Font(), tt.want)
			}
			// The header of the issues list is a title of the results: in their
			// family, and scaled as their body text is.
			header := gluttertest.ByText("Issues (0)")
			theme := m.ThemeOf(tester.Element(header))
			style := gluttertest.WidgetAs[*m.Text](tester, header).Style()
			size := theme.TextTheme.TitleSmall.FontSize.OrElse(0) * pixels(tt.want.Results.Size) / theme.TextTheme.BodyMedium.FontSize.OrElse(0)
			if tt.want.Results.Family != "" && style.FontFamily != tt.want.Results.Family {
				t.Errorf("header family %q, want %q", style.FontFamily, tt.want.Results.Family)
			}
			if math.Abs(style.FontSize.OrElse(0)-size) > 1e-9 {
				t.Errorf("header size %v, want %v", style.FontSize.OrElse(0), size)
			}
		})
	}
}

func TestPreferencesLinkInTheNoLLMMessage(t *testing.T) {
	_, w := pumpWindow(t)
	var opened int
	w.SetHandlers(Handlers{Preferences: func() { opened++ }})
	w.ClickExplanationLink(preferencesLink)
	if opened != 1 {
		t.Errorf("the link should open Preferences, opened %d", opened)
	}
	w.SetExplanation(view.ExplainReady, "x", "text")
	w.ClickExplanationLink(preferencesLink)
	if opened != 1 {
		t.Error("an explanation must not be able to open Preferences")
	}
}

func TestGeometryRoundTrip(t *testing.T) {
	_, w := pumpWindow(t)
	w.SetWindowSize(900, 600)
	g := w.Geometry()
	w.SetWindowSize(1200, 800)
	w.SetGeometry(g)
	if x, y := w.WindowSize(); x != 900 || y != 600 {
		t.Errorf("size = %dx%d", x, y)
	}
	for _, bad := range [][]byte{nil, []byte("junk"), []byte("0x5"), []byte("ax5")} {
		w.SetGeometry(bad)
		if x, y := w.WindowSize(); x != 900 || y != 600 {
			t.Errorf("%q changed the size to %dx%d", bad, x, y)
		}
	}
}

func TestSplitterSizesRoundTripAndDrag(t *testing.T) {
	tester, w := pumpWindow(t)
	sizes := w.SplitterSizes()
	if len(sizes) != 4 {
		t.Fatalf("SplitterSizes() = %v", sizes)
	}
	if sizes[0] <= sizes[1] {
		t.Errorf("the editor should be wider than the right column: %v", sizes)
	}
	w.SetSplitterSizes([]int{300, 700, 100, 100})
	tester.Pump()
	got := w.SplitterSizes()
	if got[0] >= got[1] {
		t.Errorf("after restoring 300:700 the first pane should be narrower: %v", got)
	}
	before := w.SplitterSizes()
	w.hSplit.Drag(50)
	tester.Pump()
	if after := w.SplitterSizes(); after[0] != before[0]+50 {
		t.Errorf("dragging the bar 50 pixels: %v -> %v", before, after)
	}
	w.hSplit.Drag(-10000)
	if w.SplitterSizes()[0] < int(minPane) {
		t.Errorf("a pane cannot be dragged below %v pixels: %v", minPane, w.SplitterSizes())
	}
	for _, bad := range [][]int{nil, {1, 2, 3}, {1, 2, 3, 0}, {-1, 2, 3, 4}} {
		w.SetSplitterSizes(bad)
	}
}

func TestCloseGoesToTheApplication(t *testing.T) {
	_, w := pumpWindow(t)
	var requested int
	w.SetHandlers(Handlers{CloseRequested: func() { requested++ }})
	w.Close()
	if requested != 1 || w.Quitting() {
		t.Errorf("requested %d quitting %v", requested, w.Quitting())
	}
	if resp := w.onExitRequested(); resp != ui.AppExitResponseCancel || requested != 2 {
		t.Errorf("the close button must wait for the application: %v, requested %d", resp, requested)
	}
	w.Hide()
	if w.Visible() {
		t.Error("Hide takes the window off the screen")
	}
	if c := w.windowController(); c == nil || !c.IsHidden() || c.IsMinimized() {
		t.Error("Hide must hide the platform window, not minimize it")
	}
	w.Present()
	if !w.Visible() || !w.IsActive() {
		t.Error("Present shows the window again")
	}
	if c := w.windowController(); c == nil || c.IsHidden() {
		t.Error("Present must show the platform window again")
	}
	w.Exit()
	if !w.Quitting() || w.onExitRequested() != ui.AppExitResponseExit {
		t.Error("after Exit the window lets the application end")
	}
}

func TestCloseWithoutAHandlerEndsTheApplication(t *testing.T) {
	_, w := pumpWindow(t)
	if w.onExitRequested() != ui.AppExitResponseExit {
		t.Error("with no handler the close button ends the application")
	}
	w.Close()
	if !w.Quitting() {
		t.Error("Close without a handler exits")
	}
}

func TestButtonShortcuts(t *testing.T) {
	_, w := pumpWindow(t)
	var got []string
	rec := func(name string) func() { return func() { got = append(got, name) } }
	w.SetHandlers(Handlers{Check: rec("check"), New: rec("new"), Open: rec("open"), Save: rec("save"), SaveAs: rec("saveas")})
	w.SetLLMs(choices(), 3)
	keys := w.ShortcutKeys()
	slices.Sort(keys)
	want := []string{"Ctrl+N", "Ctrl+O", "Ctrl+Return", "Ctrl+S", "Ctrl+Shift+S"}
	if !slices.Equal(keys, want) {
		t.Fatalf("shortcuts = %v, want %v", keys, want)
	}
	for _, k := range []string{"Ctrl+Return", "Ctrl+N", "Ctrl+O", "Ctrl+S", "Ctrl+Shift+S"} {
		if !w.ActivateShortcut(k) {
			t.Errorf("no shortcut %s", k)
		}
	}
	if w.ActivateShortcut("Ctrl+Q") {
		t.Error("Ctrl+Q is not a shortcut")
	}
	if !slices.Equal(got, []string{"check", "new", "open", "save", "saveas"}) {
		t.Errorf("reports = %v", got)
	}
}

func TestShortcutsFromTheKeyboard(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []string
	w.SetHandlers(Handlers{NextIssue: func() { got = append(got, "next") }, PrevIssue: func() { got = append(got, "prev") }})
	w.FocusEditor()
	tester.Pump()
	tester.SendKeyEvent(services.LogicalKeyboardKeyF7)
	tester.SendKeyDownEvent(services.LogicalKeyboardKeyShift)
	tester.SendKeyEvent(services.LogicalKeyboardKeyF7)
	tester.SendKeyUpEvent(services.LogicalKeyboardKeyShift)
	if !slices.Equal(got, []string{"next", "prev"}) {
		t.Errorf("F7 / Shift+F7 reports = %v", got)
	}
}

func TestWebServerButton(t *testing.T) {
	tester, w := pumpWindow(t)
	if w.WebButtonText() != webStartText {
		t.Fatalf("text = %q", w.WebButtonText())
	}
	w.SetWebRunning(true, "http://127.0.0.1:8080")
	tester.Pump()
	if w.WebButtonText() != webStopText {
		t.Errorf("text = %q", w.WebButtonText())
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(webStopText)); err != nil {
		t.Error(err)
	}
	var toggled int
	w.SetHandlers(Handlers{WebToggle: func() { toggled++ }})
	tapText(t, tester, webStopText)
	w.SetWebRunning(false, "")
	if toggled != 1 || w.WebButtonText() != webStartText {
		t.Errorf("toggled %d, text %q", toggled, w.WebButtonText())
	}
}

func TestErrorBanner(t *testing.T) {
	tester, w := pumpWindow(t)
	w.ShowError("The check failed: refused")
	tester.Pump()
	if w.ErrorText() == "" {
		t.Fatal("no banner")
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("The check failed: refused")); err != nil {
		t.Error(err)
	}
	tapText(t, tester, "×")
	if w.ErrorText() != "" {
		t.Errorf("the dismiss button should clear the banner, got %q", w.ErrorText())
	}
}

func TestBusyHintAppearsWhenTheCheckTakesLong(t *testing.T) {
	tester, w := pumpWindow(t)
	w.SetEditorBusy(true)
	tester.Pump()
	if !w.EditorBusy() || w.Editor().BusyHintShown() {
		t.Fatal("the cover is up without the hint")
	}
	tester.Pump(busyHintDelay - time.Second)
	if w.Editor().BusyHintShown() {
		t.Error("the hint must wait for the delay")
	}
	tester.Pump(2 * time.Second)
	if !w.Editor().BusyHintShown() {
		t.Error("the hint should show after the delay")
	}
	w.SetEditorBusy(false)
	w.SetEditorBusy(true)
	tester.Pump(busyHintDelay + time.Second)
	if !w.Editor().BusyHintShown() {
		t.Error("a new check starts its own delay")
	}
	w.SetEditorBusy(false)
	tester.Pump(busyHintDelay * 2)
	if w.Editor().BusyHintShown() {
		t.Error("the timer of a finished check must not show the hint")
	}
}

func testRows() []issues.Row {
	return []issues.Row{
		{ID: 4, Category: analysis.Grammar, Marker: "G", Snippet: "he go", Label: "Subject-verb agreement", Count: 3},
		{ID: 9, Category: analysis.Vocabulary, Marker: "V", Snippet: "<b>x</b>", Label: "Spelling"},
	}
}
