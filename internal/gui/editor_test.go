package gui

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// recorder collects what an editor reports.
type recorder struct {
	edits    []Edit
	cursors  []int
	modified []bool
	changed  int
	alt      [][2]int
	check    [][2]int
	tooltip  map[int]string
}

func (r *recorder) hooks() editorHooks {
	return editorHooks{
		Edited:          func(e Edit) { r.edits = append(r.edits, e) },
		TextChanged:     func() { r.changed++ },
		CursorMoved:     func(p int) { r.cursors = append(r.cursors, p) },
		ModifiedChanged: func(b bool) { r.modified = append(r.modified, b) },
		TooltipAt:       func(p int) string { return r.tooltip[p] },
		Alternative:     func(s, e int) { r.alt = append(r.alt, [2]int{s, e}) },
		CheckSelection:  func(s, e int) { r.check = append(r.check, [2]int{s, e}) },
	}
}

// pumpEditor shows an editor in an app of its own.
func pumpEditor(t *testing.T, r *recorder) (*gluttertest.Tester, *Editor) {
	t.Helper()
	tester := gluttertest.New(t)
	e := NewEditor(r.hooks())
	t.Cleanup(e.Dispose)
	tester.PumpWidget(m.NewMaterialApp(m.MaterialAppOpts{Home: m.NewScaffold(m.ScaffoldOpts{Body: e.Widget()})}))
	return tester, e
}

// settle lets the undo history's half-second window pass: the frame that builds
// a field starts the clock, so it takes one frame and then a second.
func settle(tester *gluttertest.Tester) {
	tester.Pump()
	tester.Pump(time.Second)
}

// styled is a stretch of the text in the field with its look.
type styled struct {
	text  string
	style *painting.TextStyle
}

// segments returns the stretches the field draws.
func segments(t *testing.T, e *Editor) []styled {
	t.Helper()
	st, ok := e.fieldKey.CurrentState()
	if !ok {
		t.Fatal("the editor is not on screen")
	}
	var out []styled
	st.RenderEditable().Text().VisitChildren(func(s painting.InlineSpan) bool {
		if ts, ok := s.(*painting.TextSpan); ok {
			if txt, has := ts.Text.Get(); has && txt != "" {
				out = append(out, styled{txt, ts.Style()})
			}
		}
		return true
	})
	return out
}

func TestTypingReportsEditsInUTF16Units(t *testing.T) {
	r := &recorder{}
	tester, e := pumpEditor(t, r)
	finder := gluttertest.ByType[*widgets.EditableText]()
	tester.EnterText(finder, "a😀")
	tester.Pump()
	if got := e.Text(); got != "a😀" {
		t.Fatalf("Text() = %q", got)
	}
	if want := (Edit{Pos: 0, Removed: 0, Added: 3}); len(r.edits) == 0 || r.edits[len(r.edits)-1] != want {
		t.Errorf("edits = %+v, want the last to be %+v", r.edits, want)
	}
	if got := e.CursorPosition(); got != 3 {
		t.Errorf("CursorPosition() = %d, want 3 (the emoji is two units)", got)
	}
	if len(r.cursors) == 0 || r.cursors[len(r.cursors)-1] != 3 {
		t.Errorf("cursor reports = %v", r.cursors)
	}
	if !e.Modified() || len(r.modified) != 1 || !r.modified[0] {
		t.Errorf("Modified() = %v, reports %v; want one report of true", e.Modified(), r.modified)
	}
	if r.changed == 0 {
		t.Error("TextChanged was not called")
	}
}

func TestLoadTextMarksSavedAndClearsHistory(t *testing.T) {
	r := &recorder{}
	tester, e := pumpEditor(t, r)
	e.TypeIntoEditor("draft")
	settle(tester)
	e.LoadText("Loaded text.")
	settle(tester)
	if e.Modified() {
		t.Error("loaded text must count as saved")
	}
	if e.Text() != "Loaded text." || e.CursorPosition() != 0 {
		t.Errorf("text %q cursor %d", e.Text(), e.CursorPosition())
	}
	e.Undo()
	tester.Pump()
	if e.Text() != "Loaded text." {
		t.Errorf("Undo went back past the loaded text: %q", e.Text())
	}
}

func TestReplaceRangeAndUndo(t *testing.T) {
	r := &recorder{}
	tester, e := pumpEditor(t, r)
	e.LoadText("I goed home")
	// The history joins changes made within half a second into one step.
	settle(tester)
	r.edits = nil
	e.ReplaceRange(2, 6, "went")
	settle(tester)
	if e.Text() != "I went home" {
		t.Fatalf("Text() = %q", e.Text())
	}
	if len(r.edits) != 1 || r.edits[0] != (Edit{Pos: 2, Removed: 4, Added: 4}) {
		t.Errorf("edits = %+v", r.edits)
	}
	if got := e.CursorPosition(); got != 6 {
		t.Errorf("the caret should follow the replacement, got %d", got)
	}
	e.Undo()
	tester.Pump()
	if e.Text() != "I goed home" {
		t.Errorf("after Undo: %q", e.Text())
	}
}

func TestUnderlinesAreDrawnAsDecorations(t *testing.T) {
	r := &recorder{}
	tester, e := pumpEditor(t, r)
	e.LoadText("I goed to 😀 home")
	// "goed" is units 2-6; "home" follows the emoji (units 13-17).
	e.SetUnderlines([]Underline{
		{Start: 2, End: 6, Category: analysis.Grammar},
		{Start: 13, End: 17, Category: analysis.Vocabulary, Selected: true},
	})
	tester.Pump()
	want := []struct {
		text  string
		deco  ui.TextDecorationStyle
		plain bool
		bg    bool
	}{
		{text: "I ", plain: true},
		{text: "goed", deco: ui.TextDecorationStyleWavy},
		{text: " to 😀 ", plain: true},
		{text: "home", deco: ui.TextDecorationStyleDashed, bg: true},
	}
	got := segments(t, e)
	if len(got) != len(want) {
		t.Fatalf("segments = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.text != w.text {
			t.Errorf("segment %d text = %q, want %q", i, g.text, w.text)
			continue
		}
		hasDeco := g.style != nil && g.style.Decoration.IsSome()
		if w.plain == hasDeco {
			t.Errorf("segment %q decorated = %v, want %v", g.text, hasDeco, !w.plain)
			continue
		}
		if w.plain {
			continue
		}
		if g.style.DecorationStyle != w.deco {
			t.Errorf("segment %q decoration style = %v, want %v", g.text, g.style.DecorationStyle, w.deco)
		}
		if has := g.style.BackgroundColor.IsSome(); has != w.bg {
			t.Errorf("segment %q has a background = %v, want %v", g.text, has, w.bg)
		}
	}
	if got := e.Underlines(); len(got) != 2 {
		t.Errorf("Underlines() = %+v", got)
	}
	e.SetUnderlines(nil)
	tester.Pump()
	if got := segments(t, e); len(got) != 1 || got[0].text != "I goed to 😀 home" {
		t.Errorf("without underlines the text is one stretch, got %+v", got)
	}
}

func TestSelectedUnderlineWinsWhereTheyOverlap(t *testing.T) {
	tester, e := pumpEditor(t, &recorder{})
	e.LoadText("abcdef")
	e.SetUnderlines([]Underline{
		{Start: 1, End: 5, Category: analysis.Grammar, Selected: true},
		{Start: 0, End: 3, Category: analysis.Syntax},
	})
	tester.Pump()
	got := segments(t, e)
	for _, s := range got {
		if s.text == "bc" && (s.style == nil || !s.style.BackgroundColor.IsSome()) {
			t.Errorf("the overlap %q should show the selected underline: %+v", s.text, s.style)
		}
	}
}

func TestHoverShowsTheTooltipOfTheTextUnderTheMouse(t *testing.T) {
	r := &recorder{tooltip: map[int]string{}}
	for i := 2; i < 6; i++ {
		r.tooltip[i] = "Use the past tense"
	}
	tester, e := pumpEditor(t, r)
	e.LoadText("I goed home")
	tester.Pump()
	field := gluttertest.ByType[*widgets.EditableText]()
	origin := tester.TopLeft(field)
	// The text starts a few pixels inside the field; hover over its second word.
	tester.Hover(ui.Offset{Dx: origin.Dx + 30, Dy: origin.Dy + 12})
	tester.Pump()
	if e.HoverText() != "Use the past tense" {
		t.Fatalf("HoverText() = %q", e.HoverText())
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Use the past tense")); err != nil {
		t.Error(err)
	}
	tester.Hover(ui.Offset{Dx: origin.Dx + 1, Dy: origin.Dy + 300})
	tester.Pump()
	if e.HoverText() != "" {
		t.Errorf("the tooltip should go when the mouse leaves the issue, got %q", e.HoverText())
	}
}

func TestBusyCoverBlocksTypingAndShowsTheHint(t *testing.T) {
	tester, e := pumpEditor(t, &recorder{})
	e.LoadText("text")
	e.SetBusy(true)
	tester.Pump()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(busyText)); err != nil {
		t.Error(err)
	}
	if err := gluttertest.FindsNothing(tester, gluttertest.ByText(busyHintText)); err != nil {
		t.Error("the hint must wait:", err)
	}
	e.TypeIntoEditor("x")
	if e.Text() != "text" {
		t.Errorf("a busy editor must ignore typing, got %q", e.Text())
	}
	e.ShowBusyHint()
	tester.Pump()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(busyHintText)); err != nil {
		t.Error(err)
	}
	e.SetBusy(false)
	tester.Pump()
	if err := gluttertest.FindsNothing(tester, gluttertest.ByText(busyText)); err != nil {
		t.Error(err)
	}
	if e.BusyHintShown() {
		t.Error("the hint goes with the cover")
	}
}

func TestMenuHasAlternativeAndCheckOnlyForASelection(t *testing.T) {
	r := &recorder{}
	tester, e := pumpEditor(t, r)
	e.LoadText("I goed home")
	tester.Pump()
	labels := func() []string {
		var out []string
		for _, it := range e.MenuItems() {
			if l, ok := it.Label.Get(); ok {
				out = append(out, l)
			}
		}
		return out
	}
	has := func(label string) bool {
		for _, l := range labels() {
			if l == label {
				return true
			}
		}
		return false
	}
	if has(alternativeLabel) || has(checkLabel) {
		t.Errorf("no selection, but the menu has %v", labels())
	}
	e.SelectRange(2, 6)
	if !has(alternativeLabel) || !has(checkLabel) {
		t.Errorf("a selection should add Alternative and Check, got %v", labels())
	}
	for _, it := range e.MenuItems() {
		if l, _ := it.Label.Get(); l == alternativeLabel {
			it.OnPressed()
		}
		if l, _ := it.Label.Get(); l == checkLabel {
			it.OnPressed()
		}
	}
	if len(r.alt) != 1 || r.alt[0] != [2]int{2, 6} || len(r.check) != 1 || r.check[0] != [2]int{2, 6} {
		t.Errorf("alternative %v, check %v", r.alt, r.check)
	}
	e.SelectRange(1, 2) // only a space
	if has(alternativeLabel) {
		t.Errorf("white space alone has nothing to rewrite, menu: %v", labels())
	}
}
