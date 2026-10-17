package issues

import (
	"slices"
	"testing"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

func item(typeID string, start, end int) Item {
	return Item{
		Issue: analysis.Issue{Text: "snippet", Type: analysis.Resolve(typeID), Message: "msg"},
		Start: start,
		End:   end,
	}
}

// Three issues, one per category, in text order: grammar [0,5), vocabulary
// [10,15), syntax [20,25).
func threeItems() []Item {
	return []Item{item("verb_tense", 0, 5), item("spelling", 10, 15), item("word_order", 20, 25)}
}

func checked(t *testing.T, items []Item) *State {
	t.Helper()
	s := New()
	rev := s.BeginCheck()
	if out := s.FinishCheck(rev, items); out.Stale || out.Discarded || out.Shown != len(items) {
		t.Fatalf("FinishCheck() = %+v", out)
	}
	return s
}

func ranges(items []Item) []Range {
	out := make([]Range, len(items))
	for i, it := range items {
		out[i] = it.Range()
	}
	return out
}

type recorder struct{ changes []Change }

func (r *recorder) observe(c Change) { r.changes = append(r.changes, c) }

func (r *recorder) kinds() []ChangeKind {
	out := make([]ChangeKind, len(r.changes))
	for i, c := range r.changes {
		out[i] = c.Kind
	}
	return out
}

func TestFinishCheckSortsAndAssignsIDs(t *testing.T) {
	s := checked(t, []Item{item("word_order", 20, 25), item("verb_tense", 0, 5), item("spelling", 10, 15)})
	got := s.Items()
	for i, wantStart := range []int{0, 10, 20} {
		if got[i].Start != wantStart || got[i].ID != i+1 {
			t.Errorf("item %d = start %d id %d, want start %d id %d", i, got[i].Start, got[i].ID, wantStart, i+1)
		}
	}
	if s.Phase() != Checked {
		t.Errorf("phase = %v", s.Phase())
	}
}

func TestPhasesAndEmptyText(t *testing.T) {
	s := New()
	if s.EmptyText() != "Press Check to find errors" || s.Header() != "Issues (0)" {
		t.Errorf("before the first check: %q, %q", s.EmptyText(), s.Header())
	}
	rev := s.BeginCheck()
	if s.Phase() != Checking || s.EmptyText() != "Checking..." {
		t.Errorf("while checking: phase %v, %q", s.Phase(), s.EmptyText())
	}
	s.FinishCheck(rev, nil)
	if s.EmptyText() != "No errors found" {
		t.Errorf("after an empty result: %q", s.EmptyText())
	}

	rev = s.BeginCheck()
	s.FinishCheck(rev, threeItems())
	if s.EmptyText() != "" {
		t.Errorf("with rows: %q", s.EmptyText())
	}
	for _, c := range analysis.Categories() {
		s.SetCategoryVisible(c, false)
	}
	if s.EmptyText() != "All categories are hidden" {
		t.Errorf("with everything hidden: %q", s.EmptyText())
	}
}

func TestOldIssuesStayWhileCheckingAndAfterAbort(t *testing.T) {
	s := checked(t, threeItems())
	s.BeginCheck()
	if len(s.Items()) != 3 || s.EmptyText() != "" {
		t.Error("existing issues must stay visible while a new check runs")
	}
	s.AbortCheck()
	if s.Phase() != Checked || len(s.Items()) != 3 {
		t.Errorf("after abort: phase %v, %d items", s.Phase(), len(s.Items()))
	}
	// Aborting a first check returns to "not checked".
	fresh := New()
	fresh.BeginCheck()
	fresh.AbortCheck()
	if fresh.Phase() != NotChecked {
		t.Errorf("phase = %v, want NotChecked", fresh.Phase())
	}
	fresh.AbortCheck() // harmless when nothing is pending
}

func TestStaleResultsAreIgnored(t *testing.T) {
	s := New()
	first := s.BeginCheck()
	s.ApplyEdit(Edit{Pos: 50, Added: 1}) // the document moves on
	second := s.BeginCheck()
	if first == second {
		t.Fatal("a newer check must have a newer revision")
	}
	if out := s.FinishCheck(first, threeItems()); !out.Stale || len(s.Items()) != 0 {
		t.Errorf("superseded result applied: %+v", out)
	}
	if out := s.FinishCheck(second, threeItems()); out.Stale || out.Shown != 3 {
		t.Errorf("current result: %+v", out)
	}
	if out := s.FinishCheck(second, threeItems()); !out.Stale {
		t.Error("a result can be applied only once")
	}
	if out := New().FinishCheck(0, threeItems()); !out.Stale {
		t.Error("a result without a pending check is stale")
	}
}

func TestResultIsRebasedAcrossEditsMadeDuringTheCheck(t *testing.T) {
	s := New()
	rev := s.BeginCheck()
	s.ApplyEdit(Edit{Pos: 7, Removed: 0, Added: 2})  // between items 1 and 2: items 2 and 3 move by +2
	s.ApplyEdit(Edit{Pos: 30, Removed: 2, Added: 0}) // after everything: ignored
	s.ApplyEdit(Edit{Pos: 14, Removed: 0, Added: 1}) // inside item 2 (now 12..17): dropped, and item 3 moves by +1
	s.ApplyEdit(Edit{Pos: 18, Removed: 1, Added: 1}) // ends before item 3 (now 23..28): no shift

	out := s.FinishCheck(rev, threeItems())
	if out.Shown != 2 || out.Dropped != 1 || out.Stale || out.Discarded {
		t.Fatalf("outcome = %+v", out)
	}
	want := []Range{{0, 5}, {23, 28}} // item 3: +2 from the first edit, +1 from the third
	if got := ranges(s.Items()); !slices.Equal(got, want) {
		t.Errorf("ranges = %v, want %v", got, want)
	}
	for i, it := range s.Items() {
		if it.ID != i+1 {
			t.Errorf("IDs must be 1..N after dropping, got %d at %d", it.ID, i)
		}
	}
}

func TestEditsWhileNoCheckIsPendingAreNotLogged(t *testing.T) {
	s := New()
	for range 10 {
		s.ApplyEdit(Edit{Pos: 0, Added: 1})
	}
	if len(s.log) != 0 {
		t.Errorf("log has %d edits, want 0", len(s.log))
	}
	if s.Revision() != 10 {
		t.Errorf("revision = %d, want 10", s.Revision())
	}
}

func TestTooManyEditsDuringACheckDiscardsTheResult(t *testing.T) {
	s := checked(t, threeItems())
	rev := s.BeginCheck()
	for range maxPendingEdits + 1 {
		s.ApplyEdit(Edit{Pos: 1000, Added: 1})
	}
	out := s.FinishCheck(rev, []Item{item("article", 0, 3)})
	if !out.Discarded {
		t.Fatalf("outcome = %+v, want discarded", out)
	}
	if len(s.Items()) != 3 || s.Phase() != Checked {
		t.Errorf("the old issues must stay: %d items, phase %v", len(s.Items()), s.Phase())
	}
}

func TestApplyEditMovesAndRemovesIssues(t *testing.T) {
	tests := []struct {
		name        string
		edit        Edit
		wantRanges  []Range
		wantRemoved []int
	}{
		{"edit after everything", Edit{Pos: 40, Added: 3}, []Range{{0, 5}, {10, 15}, {20, 25}}, nil},
		{"insert between first and second", Edit{Pos: 7, Added: 2}, []Range{{0, 5}, {12, 17}, {22, 27}}, nil},
		{"delete between first and second", Edit{Pos: 7, Removed: 2}, []Range{{0, 5}, {8, 13}, {18, 23}}, nil},
		{"touch the second", Edit{Pos: 15, Added: 1}, []Range{{0, 5}, {21, 26}}, []int{2}},
		{"one edit spanning two issues", Edit{Pos: 3, Removed: 10}, []Range{{20 - 10, 25 - 10}}, []int{1, 2}},
		{"edit at the very start touches the first", Edit{Pos: 0, Added: 1}, []Range{{11, 16}, {21, 26}}, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := checked(t, threeItems())
			rec := &recorder{}
			s.Subscribe(rec.observe)
			s.ApplyEdit(tt.edit)
			if got := ranges(s.Items()); !slices.Equal(got, tt.wantRanges) {
				t.Errorf("ranges = %v, want %v", got, tt.wantRanges)
			}
			if len(tt.wantRemoved) == 0 && tt.edit.Pos > 30 {
				if len(rec.changes) != 0 {
					t.Errorf("an edit after every issue must not notify, got %v", rec.changes)
				}
				return
			}
			if len(rec.changes) == 0 || rec.changes[0].Kind != IssuesChanged || !slices.Equal(rec.changes[0].RemovedIDs, tt.wantRemoved) {
				t.Errorf("changes = %+v, want IssuesChanged removing %v", rec.changes, tt.wantRemoved)
			}
		})
	}
}

func TestRemovingTheSelectedIssueClearsTheSelection(t *testing.T) {
	s := checked(t, threeItems())
	s.Select(2)
	rec := &recorder{}
	s.Subscribe(rec.observe)
	s.ApplyEdit(Edit{Pos: 12, Added: 1})
	if _, ok := s.Selected(); ok {
		t.Error("the selected issue was removed, so nothing should be selected")
	}
	if !slices.Equal(rec.kinds(), []ChangeKind{IssuesChanged, SelectionChanged}) {
		t.Errorf("changes = %v", rec.kinds())
	}
}

func TestSelection(t *testing.T) {
	s := checked(t, threeItems())
	rec := &recorder{}
	s.Subscribe(rec.observe)

	if _, ok := s.Selected(); ok {
		t.Error("nothing is selected after a check")
	}
	if !s.Select(2) {
		t.Fatal("Select(2) failed")
	}
	if it, ok := s.Selected(); !ok || it.ID != 2 {
		t.Errorf("selected = %+v, %v", it, ok)
	}
	s.Select(2) // same again: no new notification
	if s.Select(99) {
		t.Error("selecting an unknown ID must fail")
	}
	if it, _ := s.Selected(); it.ID != 2 {
		t.Error("a failed Select must not change the selection")
	}
	s.ClearSelection()
	s.ClearSelection()
	if !slices.Equal(rec.kinds(), []ChangeKind{SelectionChanged, SelectionChanged}) {
		t.Errorf("changes = %v, want one per real change", rec.kinds())
	}
}

func TestSelectAt(t *testing.T) {
	s := checked(t, threeItems())
	tests := []struct {
		pos    int
		wantID int // 0 for none
	}{
		{0, 1}, {4, 1}, {5, 0}, {9, 0}, {10, 2}, {14, 2}, {15, 0}, {20, 3}, {24, 3}, {25, 0}, {-1, 0}, {500, 0},
	}
	for _, tt := range tests {
		got := s.SelectAt(tt.pos)
		sel, ok := s.Selected()
		switch {
		case tt.wantID == 0 && (got || ok):
			t.Errorf("SelectAt(%d) selected %+v", tt.pos, sel)
		case tt.wantID != 0 && (!got || sel.ID != tt.wantID):
			t.Errorf("SelectAt(%d) = %v, selected %d, want %d", tt.pos, got, sel.ID, tt.wantID)
		}
	}
}

func TestVisibilityAndHeader(t *testing.T) {
	s := checked(t, threeItems())
	rec := &recorder{}
	s.Subscribe(rec.observe)
	if s.Header() != "Issues (3)" {
		t.Errorf("header = %q", s.Header())
	}

	s.Select(2) // the vocabulary issue
	s.SetCategoryVisible(analysis.Vocabulary, false)
	if _, ok := s.Selected(); ok {
		t.Error("hiding the selected issue's category must clear the selection")
	}
	if s.Header() != "Issues (2 of 3 shown)" {
		t.Errorf("header = %q", s.Header())
	}
	if got := len(s.Visible()); got != 2 {
		t.Errorf("visible = %d, want 2", got)
	}
	if len(s.Items()) != 3 {
		t.Error("Items() must still list hidden issues")
	}
	if s.Select(2) {
		t.Error("a hidden issue cannot be selected")
	}
	if s.SelectAt(12) {
		t.Error("a hidden issue cannot be selected from the text")
	}
	if s.CategoryVisible(analysis.Vocabulary) || !s.CategoryVisible(analysis.Grammar) {
		t.Error("CategoryVisible is wrong")
	}

	s.SetCategoryVisible(analysis.Vocabulary, false) // unchanged: no notification
	s.SetCategoryVisible(analysis.Vocabulary, true)
	if s.Header() != "Issues (3)" || !s.Select(2) {
		t.Error("showing the category again restores the issues")
	}
	want := []ChangeKind{SelectionChanged, VisibilityChanged, SelectionChanged, VisibilityChanged, SelectionChanged}
	if !slices.Equal(rec.kinds(), want) {
		t.Errorf("changes = %v, want %v", rec.kinds(), want)
	}
}

func TestNextAndPrev(t *testing.T) {
	s := checked(t, threeItems())
	ids := func(it Item, ok bool) int {
		if !ok {
			return 0
		}
		return it.ID
	}
	// Nothing selected: relative to the cursor.
	if got := ids(s.Next(12)); got != 3 {
		t.Errorf("Next from cursor 12 selected %d, want 3", got)
	}
	s.ClearSelection()
	if got := ids(s.Next(99)); got != 1 {
		t.Errorf("Next past the last issue should wrap to 1, got %d", got)
	}
	s.ClearSelection()
	if got := ids(s.Prev(12)); got != 2 {
		t.Errorf("Prev from cursor 12 selected %d, want 2 (starts at 10)", got)
	}
	s.ClearSelection()
	if got := ids(s.Prev(0)); got != 3 {
		t.Errorf("Prev before the first issue should wrap to 3, got %d", got)
	}

	// With a selection the cursor is ignored.
	s.Select(1)
	for _, want := range []int{2, 3, 1} {
		if got := ids(s.Next(0)); got != want {
			t.Fatalf("Next selected %d, want %d", got, want)
		}
	}
	for _, want := range []int{3, 2, 1} {
		if got := ids(s.Prev(0)); got != want {
			t.Fatalf("Prev selected %d, want %d", got, want)
		}
	}

	// Hidden issues are skipped.
	s.SetCategoryVisible(analysis.Vocabulary, false)
	s.Select(1)
	if got := ids(s.Next(0)); got != 3 {
		t.Errorf("Next should skip the hidden issue, got %d", got)
	}

	if _, ok := New().Next(0); ok {
		t.Error("Next with no issues must report false")
	}
	if _, ok := New().Prev(0); ok {
		t.Error("Prev with no issues must report false")
	}
}

func TestRemove(t *testing.T) {
	s := checked(t, threeItems())
	s.Select(2)
	rec := &recorder{}
	s.Subscribe(rec.observe)
	if !s.Remove(2) {
		t.Fatal("Remove(2) failed")
	}
	if s.Remove(2) || s.Remove(99) {
		t.Error("removing a missing issue must report false")
	}
	if got := ranges(s.Items()); !slices.Equal(got, []Range{{0, 5}, {20, 25}}) {
		t.Errorf("ranges = %v", got)
	}
	if _, ok := s.Selected(); ok {
		t.Error("the removed issue was selected")
	}
	if len(rec.changes) != 2 || rec.changes[0].Kind != IssuesChanged || !slices.Equal(rec.changes[0].RemovedIDs, []int{2}) || rec.changes[1].Kind != SelectionChanged {
		t.Errorf("changes = %+v", rec.changes)
	}
	// IDs of the other issues do not change, so caches keyed by them stay valid.
	if _, ok := s.Get(3); !ok {
		t.Error("ID 3 must still exist")
	}
}

func TestNewResultReplacesEverything(t *testing.T) {
	s := checked(t, threeItems())
	s.Select(1)
	firstCheck := s.CheckID()
	rec := &recorder{}
	s.Subscribe(rec.observe)

	rev := s.BeginCheck()
	s.FinishCheck(rev, []Item{item("article", 3, 6)})
	if s.CheckID() == firstCheck {
		t.Error("CheckID must change with every result")
	}
	if _, ok := s.Selected(); ok {
		t.Error("a new result clears the selection")
	}
	var removed []int
	for _, c := range rec.changes {
		if c.Kind == IssuesChanged {
			removed = c.RemovedIDs
		}
	}
	if !slices.Equal(removed, []int{1, 2, 3}) {
		t.Errorf("removed IDs = %v, want the three old ones", removed)
	}
}

func TestUnsubscribe(t *testing.T) {
	s := New()
	rec := &recorder{}
	stop := s.Subscribe(rec.observe)
	s.BeginCheck()
	n := len(rec.changes)
	stop()
	s.AbortCheck()
	if len(rec.changes) != n {
		t.Error("an unsubscribed observer must not be called")
	}
}

func TestObserversRunInRegistrationOrder(t *testing.T) {
	s := New()
	var order []int
	for i := 1; i <= 5; i++ {
		s.Subscribe(func(Change) { order = append(order, i) })
	}
	s.BeginCheck()
	if !slices.Equal(order, []int{1, 2, 3, 4, 5}) {
		t.Errorf("order = %v", order)
	}
}

func TestItemsReturnsACopy(t *testing.T) {
	s := checked(t, threeItems())
	s.Items()[0].Start = 999
	if s.Items()[0].Start == 999 {
		t.Error("Items() exposes the internal slice")
	}
}

func TestFromPlaced(t *testing.T) {
	// 😀 is 4 bytes and 2 UTF-16 units; é is 2 bytes and 1 unit.
	text := "😀 é he go"
	start := len("😀 é ")
	placed := []analysis.Placed{{
		Issue: analysis.Issue{Text: "he go", Type: analysis.Resolve("verb_form")},
		Start: start,
		End:   start + len("he go"),
	}}
	got := FromPlaced(text, placed)
	if len(got) != 1 || got[0].Start != 5 || got[0].End != 10 {
		t.Errorf("FromPlaced() = %+v, want UTF-16 range [5,10)", got)
	}
	if got[0].Text != "he go" || got[0].Category() != analysis.Grammar {
		t.Errorf("issue data lost: %+v", got[0])
	}
	if want := analysis.Fingerprint(text, start, start+len("he go")); got[0].Fingerprint != want {
		t.Errorf("fingerprint = %q, want the one computed from the byte offsets %q", got[0].Fingerprint, want)
	}
}

func TestReset(t *testing.T) {
	s := checked(t, threeItems())
	s.Select(2)
	s.SetCategoryVisible(analysis.Syntax, false)
	checkID := s.CheckID()
	rec := &recorder{}
	s.Subscribe(rec.observe)

	s.Reset()
	if len(s.Items()) != 0 || s.Phase() != NotChecked || s.EmptyText() != "Press Check to find errors" {
		t.Errorf("items %d, phase %v, empty text %q", len(s.Items()), s.Phase(), s.EmptyText())
	}
	if _, ok := s.Selected(); ok {
		t.Error("the selection must go")
	}
	if s.CheckID() == checkID {
		t.Error("CheckID must change, so cached explanations of the old text are not reused")
	}
	if s.CategoryVisible(analysis.Syntax) {
		t.Error("the user's choice of hidden categories is kept")
	}
	if !slices.Equal(rec.kinds(), []ChangeKind{PhaseChanged, IssuesChanged, SelectionChanged}) {
		t.Errorf("changes = %v", rec.kinds())
	}
	if len(rec.changes[1].RemovedIDs) != 3 {
		t.Errorf("removed IDs = %v", rec.changes[1].RemovedIDs)
	}
}

func TestResetDropsAPendingCheck(t *testing.T) {
	s := New()
	rev := s.BeginCheck()
	s.Reset()
	if out := s.FinishCheck(rev, threeItems()); !out.Stale {
		t.Errorf("a result for the text before the reset is stale: %+v", out)
	}
	if len(s.Items()) != 0 || s.Phase() != NotChecked {
		t.Error("a stale result must change nothing")
	}
	// A fresh check works afterwards.
	rev = s.BeginCheck()
	if out := s.FinishCheck(rev, threeItems()); out.Shown != 3 {
		t.Errorf("outcome = %+v", out)
	}
}

func TestFinishRangeCheck(t *testing.T) {
	// threeItems: grammar [0,5), vocabulary [10,15), syntax [20,25).
	tests := []struct {
		name    string
		checked Range
		result  []Item
		want    []Range
	}{
		{"nothing found clears the range only", Range{8, 17}, nil, []Range{{0, 5}, {20, 25}}},
		{"the result replaces the issues in the range", Range{8, 17}, []Item{item("article", 11, 13)}, []Range{{0, 5}, {11, 13}, {20, 25}}},
		{"a result before the others keeps the order", Range{26, 40}, []Item{item("article", 30, 33)}, []Range{{0, 5}, {10, 15}, {20, 25}, {30, 33}}},
		{"an issue the range cuts in half is replaced", Range{12, 22}, []Item{item("article", 16, 18)}, []Range{{0, 5}, {16, 18}}},
		{"an issue that only touches the range stays", Range{5, 10}, []Item{item("article", 6, 8)}, []Range{{0, 5}, {6, 8}, {10, 15}, {20, 25}}},
		{"the whole text is a full check", Range{0, 30}, []Item{item("article", 1, 2)}, []Range{{1, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := checked(t, threeItems())
			rev := s.BeginCheck()
			out := s.FinishRangeCheck(rev, tt.checked, tt.result)
			if out.Stale || out.Discarded || out.Shown != len(tt.result) {
				t.Fatalf("outcome = %+v", out)
			}
			if got := ranges(s.Items()); !slices.Equal(got, tt.want) {
				t.Errorf("ranges = %v, want %v", got, tt.want)
			}
			for i, it := range s.Items() {
				if it.ID != i+1 {
					t.Errorf("IDs must be 1..N, got %d at %d", it.ID, i)
				}
			}
			if s.Phase() != Checked {
				t.Errorf("phase = %v, want Checked", s.Phase())
			}
		})
	}
}

func TestFinishRangeCheckOnAnUncheckedText(t *testing.T) {
	s := New()
	rev := s.BeginCheck()
	s.FinishRangeCheck(rev, Range{3, 9}, []Item{item("article", 4, 6)})
	if got := ranges(s.Items()); !slices.Equal(got, []Range{{4, 6}}) || s.Phase() != Checked {
		t.Errorf("ranges %v, phase %v", got, s.Phase())
	}
}

func TestFinishRangeCheckAnnouncesANewResult(t *testing.T) {
	s := checked(t, threeItems())
	s.Select(2)
	firstCheck := s.CheckID()
	rec := &recorder{}
	s.Subscribe(rec.observe)

	rev := s.BeginCheck()
	s.FinishRangeCheck(rev, Range{0, 8}, nil)
	if s.CheckID() == firstCheck {
		t.Error("IDs are renumbered, so CheckID must change")
	}
	if _, ok := s.Selected(); ok {
		t.Error("a new result clears the selection")
	}
	var removed []int
	for _, c := range rec.changes {
		if c.Kind == IssuesChanged {
			removed = c.RemovedIDs
		}
	}
	if !slices.Equal(removed, []int{1, 2, 3}) {
		t.Errorf("removed IDs = %v, want the three old ones", removed)
	}
}

func TestFinishRangeCheckRebasesAcrossEditsMadeDuringTheCheck(t *testing.T) {
	s := checked(t, threeItems())
	rev := s.BeginCheck()
	s.ApplyEdit(Edit{Pos: 7, Removed: 0, Added: 2}) // before the range: everything after moves by 2
	// The range [8,17) is now [10,19); the result was made for the old text.
	out := s.FinishRangeCheck(rev, Range{8, 17}, []Item{item("article", 11, 13)})
	if out.Discarded || out.Dropped != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	want := []Range{{0, 5}, {13, 15}, {22, 27}}
	if got := ranges(s.Items()); !slices.Equal(got, want) {
		t.Errorf("ranges = %v, want %v", got, want)
	}
}

func TestFinishRangeCheckIsDiscardedWhenAnEditTouchesTheRange(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit Edit
	}{
		{"inside", Edit{Pos: 12, Removed: 1, Added: 1}},
		{"right after it", Edit{Pos: 17, Removed: 0, Added: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := checked(t, threeItems())
			rev := s.BeginCheck()
			s.ApplyEdit(tt.edit)
			before := ranges(s.Items())
			out := s.FinishRangeCheck(rev, Range{8, 17}, []Item{item("article", 11, 13)})
			if !out.Discarded {
				t.Fatalf("outcome = %+v, want Discarded", out)
			}
			if got := ranges(s.Items()); !slices.Equal(got, before) {
				t.Errorf("the old issues must stay: %v -> %v", before, got)
			}
			if s.Phase() != Checked {
				t.Errorf("phase = %v, want the phase from before the check", s.Phase())
			}
		})
	}
}

func TestFinishRangeCheckIgnoresStaleResults(t *testing.T) {
	s := checked(t, threeItems())
	old := s.BeginCheck()
	s.ApplyEdit(Edit{Pos: 50, Added: 1})
	s.BeginCheck()
	if out := s.FinishRangeCheck(old, Range{0, 5}, nil); !out.Stale {
		t.Errorf("outcome = %+v, want Stale", out)
	}
	if got := ranges(s.Items()); !slices.Equal(got, []Range{{0, 5}, {10, 15}, {20, 25}}) {
		t.Errorf("a stale result must change nothing, got %v", got)
	}
}
