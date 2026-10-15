package ui

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
)

// utf16Slice returns text[u1:u2] where the offsets are UTF-16 units, the way
// Qt counts positions.
func utf16Slice(text string, u1, u2 int) string {
	m := analysis.NewUTF16Map(text)
	return text[m.ByteOffset(u1):m.ByteOffset(u2)]
}

func TestSetUnderlines(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		text := "He go to school. I very like it. Make a photo."
		w.SetText(text)
		span := func(s string) (int, int) { i := strings.Index(text, s); return i, i + len(s) }
		g0, g1 := span("He go")
		s0, s1 := span("I very like it")
		v0, v1 := span("Make a photo")
		w.SetUnderlines([]Underline{
			{Start: g0, End: g1, Category: analysis.Grammar},
			{Start: s0, End: s1, Category: analysis.Syntax, Selected: true},
			{Start: v0, End: v1, Category: analysis.Vocabulary},
		})

		sels := w.editor.ExtraSelections()
		if len(sels) != 3 {
			t.Fatalf("extra selections = %d, want 3", len(sels))
		}
		// The selected underline is drawn last, so its background wins.
		last := sels[2]
		if got := last.Cursor().SelectedText(); got != "I very like it" {
			t.Errorf("last selection covers %q, want the selected one", got)
		}

		type look struct {
			text  string
			style qt.QTextCharFormat__UnderlineStyle
			color string
			bg    bool
		}
		var got []look
		for _, s := range sels {
			f := s.Format()
			got = append(got, look{s.Cursor().SelectedText(), f.UnderlineStyle(), strings.ToUpper(f.UnderlineColor().Name()), f.HasProperty(int(qt.QTextFormat__BackgroundBrush))})
		}
		dark := w.isDark()
		want := []look{
			{"He go", qt.QTextCharFormat__WaveUnderline, CategoryColor(analysis.Grammar, dark), false},
			{"Make a photo", qt.QTextCharFormat__DashUnderline, CategoryColor(analysis.Vocabulary, dark), false},
			{"I very like it", qt.QTextCharFormat__DotLine, CategoryColor(analysis.Syntax, dark), true},
		}
		if !slices.Equal(got, want) {
			t.Errorf("looks = %+v\nwant   %+v", got, want)
		}
		bg := last.Format().Background().Color()
		if bg.Alpha() != selectedAlpha {
			t.Errorf("selected background alpha = %d, want about 20%% (%d)", bg.Alpha(), selectedAlpha)
		}

		if len(w.Underlines()) != 3 {
			t.Error("Underlines() should return what was set")
		}
		w.SetUnderlines(nil)
		if len(w.editor.ExtraSelections()) != 0 {
			t.Error("clearing the underlines must clear the extra selections")
		}
	})
}

func TestUnderlinesDoNotTouchTheTextOrTheUndoStack(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetText("He go home")
		w.editor.Document().ClearUndoRedoStacks()
		edits := 0
		w.SetHandlers(Handlers{TextEdited: func(int, int, int) { edits++ }})
		w.SetUnderlines([]Underline{{Start: 0, End: 5, Category: analysis.Grammar}})
		w.SetUnderlines([]Underline{{Start: 3, End: 5, Category: analysis.Vocabulary, Selected: true}})
		if w.Text() != "He go home" || w.editor.Document().IsUndoAvailable() || w.editor.Document().IsModified() && edits > 0 {
			t.Errorf("underlines changed the document: text=%q undo=%v edits=%d", w.Text(), w.editor.Document().IsUndoAvailable(), edits)
		}
		if edits != 0 {
			t.Errorf("underlines reported %d text edits", edits)
		}
	})
}

func TestUnderlinesUseUTF16Positions(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		text := "😀 café he go 😀"
		w.SetText(text)
		start := analysis.NewUTF16Map(text).Offset(strings.Index(text, "he go"))
		w.SetUnderlines([]Underline{{Start: start, End: start + 5, Category: analysis.Grammar}})
		sels := w.editor.ExtraSelections()
		if len(sels) != 1 || sels[0].Cursor().SelectedText() != "he go" {
			t.Errorf("underline covers %q, want %q", sels[0].Cursor().SelectedText(), "he go")
		}
	})
}

func TestTextEditedReportsQtEdits(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetText("one two three")
		type edit struct{ pos, removed, added int }
		var got []edit
		w.SetHandlers(Handlers{TextEdited: func(p, r, a int) { got = append(got, edit{p, r, a}) }})

		c := w.editor.TextCursor()
		c.SetPosition(4)
		c.InsertText("XX") // insert in the middle
		c.SetPosition(0)
		c.SetPosition2(3, qt.QTextCursor__KeepAnchor)
		c.RemoveSelectedText() // delete the first word
		c.SetPosition(2)
		c.SetPosition2(5, qt.QTextCursor__KeepAnchor)
		c.InsertText("🙂") // replace three units by an emoji (two units)

		want := []edit{{4, 0, 2}, {0, 3, 0}, {2, 3, 2}}
		if !slices.Equal(got, want) {
			t.Errorf("edits = %+v, want %+v", got, want)
		}
	})
}

// TestIssueStateFollowsRealQtEdits is the end-to-end check of section 5.5: the
// issue state is fed by the editor's real contentsChange signal, through random
// edits, and every issue that survives must still cover exactly the words it was
// put on. The text has emoji and accents, so UTF-16 positions matter.
func TestIssueStateFollowsRealQtEdits(t *testing.T) {
	for seed := range uint64(40) {
		withWindow(t, func(w *MainWindow) {
			r := rand.New(rand.NewPCG(seed, 7))
			text := "Ünï 😀 he go to school. 😀😀 She have a cat. I very like it, 😀 and they is happy."
			w.SetText(text)

			state := issues.New()
			rev := state.BeginCheck()
			words := []string{"he go", "She have", "I very like it", "they is"}
			m := analysis.NewUTF16Map(text)
			var items []issues.Item
			for _, word := range words {
				b := strings.Index(text, word)
				items = append(items, issues.Item{
					Issue: analysis.Issue{Text: word, Type: analysis.Resolve("verb_form")},
					Start: m.Offset(b), End: m.Offset(b + len(word)),
				})
			}
			state.FinishCheck(rev, items)
			w.SetHandlers(Handlers{TextEdited: func(p, rem, add int) {
				state.ApplyEdit(issues.Edit{Pos: p, Removed: rem, Added: add})
			}})

			insertions := []string{"x", "ab", "🙂", "é\n", " "}
			for step := range 25 {
				now := w.Text()
				runes := []rune(now)
				if len(runes) < 1 {
					break
				}
				// Cursors sit between characters, never inside a surrogate pair.
				um := analysis.NewUTF16Map(now)
				unitAt := func(runeIndex int) int { return um.Offset(len(string(runes[:runeIndex]))) }
				first := r.IntN(len(runes) + 1)
				c := w.editor.TextCursor()
				c.SetPosition(unitAt(first))
				if r.IntN(2) == 0 {
					c.SetPosition2(unitAt(min(len(runes), first+r.IntN(4))), qt.QTextCursor__KeepAnchor)
				}
				c.InsertText(insertions[r.IntN(len(insertions))])

				now = w.Text()
				for _, it := range state.Items() {
					if got := utf16Slice(now, it.Start, it.End); got != it.Text {
						t.Errorf("seed %d step %d: issue %q now covers %q in %q", seed, step, it.Text, got, now)
						return
					}
				}
			}
		})
	}
}

func TestCursorMoved(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetText("He go home")
		var got []int
		w.SetHandlers(Handlers{CursorMoved: func(p int) { got = append(got, p) }})
		w.MoveCursorTo(3)
		w.MoveCursorTo(7)
		if !slices.Equal(got, []int{3, 7}) || w.CursorPosition() != 7 {
			t.Errorf("moves = %v, cursor = %d", got, w.CursorPosition())
		}
	})
}

func TestMoveCursorToKeepsTheFocus(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetText("He go home")
		w.Show()
		w.win.ActivateWindow()
		w.issuesList.SetFocus()
		qt.QCoreApplication_ProcessEvents()
		if !w.issuesList.HasFocus() {
			t.Skip("the offscreen platform did not give the list focus")
		}
		w.MoveCursorTo(4)
		if !w.issuesList.HasFocus() || w.editor.HasFocus() {
			t.Error("moving the cursor must not take the focus from the list")
		}
		w.FocusEditor()
		if !w.editor.HasFocus() {
			t.Error("FocusEditor must give the editor the focus")
		}
	})
}

func TestTooltipLookup(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		w.SetText("He go to school")
		w.win.Resize(600, 300)
		w.Show()
		qt.QCoreApplication_ProcessEvents()

		var asked []int
		w.SetHandlers(Handlers{TooltipAt: func(pos int) string {
			asked = append(asked, pos)
			if pos >= 3 && pos < 5 {
				return "tip for go"
			}
			return ""
		}})

		cur := w.editor.TextCursor()
		cur.SetPosition(4)
		rect := w.editor.CursorRect(cur)
		p := qt.NewQPoint2(rect.X(), rect.Y()+rect.Height()/2)
		help := qt.NewQHelpEvent(qt.QEvent__ToolTip, p, w.editor.Viewport().MapToGlobalWithQPoint(p))
		superCalled := false
		handled := w.viewportEvent(func(*qt.QEvent) bool { superCalled = true; return false }, help.QEvent)
		if !handled || superCalled {
			t.Errorf("handled=%v superCalled=%v: a tooltip event is answered by the window", handled, superCalled)
		}
		if len(asked) != 1 || asked[0] != 4 {
			t.Errorf("the handler was asked about %v, want [4]", asked)
		}

		// Another event type goes to Qt's own handling.
		other := qt.NewQEvent(qt.QEvent__Enter)
		w.viewportEvent(func(*qt.QEvent) bool { superCalled = true; return false }, other)
		if !superCalled {
			t.Error("other events must be passed on")
		}

		// With no handler the event is passed on as well.
		w.SetHandlers(Handlers{})
		superCalled = false
		w.viewportEvent(func(*qt.QEvent) bool { superCalled = true; return false }, help.QEvent)
		if !superCalled {
			t.Error("without a handler the tooltip event is passed on")
		}
	})
}

func TestIssueShortcuts(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var calls []string
		w.SetHandlers(Handlers{
			NextIssue: func() { calls = append(calls, "next") },
			PrevIssue: func() { calls = append(calls, "prev") },
		})
		w.nextShortcut.Activated()
		w.prevShortcut.Activated()
		if !slices.Equal(calls, []string{"next", "prev"}) {
			t.Errorf("calls = %v", calls)
		}
		if got := w.nextShortcut.Key().ToString(); got != "F7" {
			t.Errorf("next shortcut = %q", got)
		}
		if got := w.prevShortcut.Key().ToString(); got != "Shift+F7" {
			t.Errorf("previous shortcut = %q", got)
		}
	})
}

func TestErrorBanner(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		if !w.banner.IsHidden() {
			t.Error("the banner starts hidden")
		}
		w.ShowError("The request to local failed: connection refused")
		if w.banner.IsHidden() || w.bannerLabel.Text() != "The request to local failed: connection refused" {
			t.Errorf("banner hidden=%v text=%q", w.banner.IsHidden(), w.bannerLabel.Text())
		}
		w.ClearError()
		if !w.banner.IsHidden() || w.bannerLabel.Text() != "" {
			t.Error("ClearError must hide the banner")
		}
	})
}
