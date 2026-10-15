package issues

import (
	"slices"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// Item is a placed issue. Start and End are UTF-16 offsets in the current
// document; the State keeps them up to date as the text is edited.
type Item struct {
	// ID identifies the item within one check result (1, 2, ... in text order).
	ID int
	analysis.Issue
	Start int
	End   int
	// Fingerprint identifies the mistake within the checked text (section 5.7).
	// It is computed from the text at check time, because later edits would
	// change the occurrence index.
	Fingerprint string
}

// Range returns the item's range.
func (it Item) Range() Range { return Range{it.Start, it.End} }

// Category returns the item's error category.
func (it Item) Category() analysis.Category { return it.Type.Category }

// FromPlaced converts the byte offsets of placed issues, which refer to text,
// into UTF-16 offsets and computes their fingerprints. IDs are left for the
// State to assign.
func FromPlaced(text string, placed []analysis.Placed) []Item {
	m := analysis.NewUTF16Map(text)
	items := make([]Item, len(placed))
	for i, p := range placed {
		items[i] = Item{
			Issue:       p.Issue,
			Start:       m.Offset(p.Start),
			End:         m.Offset(p.End),
			Fingerprint: analysis.Fingerprint(text, p.Start, p.End),
		}
	}
	return items
}

// Phase says what the issues list has to show when it has no rows.
type Phase int

// The phases of the state.
const (
	NotChecked Phase = iota // no check has finished yet
	Checking                // a check is running
	Checked                 // the last check finished
)

// ChangeKind says what changed in a State.
type ChangeKind int

// The kinds of change an observer is told about.
const (
	// IssuesChanged: a new result, an issue removed, or issues moved by an edit.
	IssuesChanged ChangeKind = iota
	// SelectionChanged: the selected issue changed (possibly to none).
	SelectionChanged
	// VisibilityChanged: a category was shown or hidden.
	VisibilityChanged
	// PhaseChanged: the phase changed (see Phase).
	PhaseChanged
)

// Change is passed to observers after the state has changed.
type Change struct {
	Kind ChangeKind
	// RemovedIDs lists the issues an IssuesChanged change removed, so that
	// work for them (such as a running explanation) can be cancelled.
	RemovedIDs []int
}

// maxPendingEdits bounds the edit log kept while a check runs. Past it, the
// result is discarded rather than rebased.
const maxPendingEdits = 100_000

// State is the issue state of one document. It is not safe for concurrent use:
// call it from the UI thread only. Observers are called synchronously and
// must not change the state they are told about.
type State struct {
	items    []Item // sorted by Start, no overlaps
	selected int    // ID of the selected item, 0 for none
	hidden   map[analysis.Category]bool
	phase    Phase
	// beforeCheck is the phase to return to if a check ends without a result.
	beforeCheck Phase
	checkID     uint64
	revision    uint64

	pending      bool
	pendingRev   uint64
	log          []Edit
	logOverflown bool

	observers map[int]func(Change)
	nextObs   int
}

// New returns an empty State: nothing checked, every category visible.
func New() *State {
	return &State{hidden: map[analysis.Category]bool{}, observers: map[int]func(Change){}}
}

// Subscribe registers fn to be called after every change. The returned
// function removes the observer.
func (s *State) Subscribe(fn func(Change)) (unsubscribe func()) {
	s.nextObs++
	id := s.nextObs
	s.observers[id] = fn
	return func() { delete(s.observers, id) }
}

func (s *State) notify(c Change) {
	// Map order is random; sort by registration order so observers run in a
	// predictable order.
	ids := make([]int, 0, len(s.observers))
	for id := range s.observers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if fn, ok := s.observers[id]; ok {
			fn(c)
		}
	}
}

// Phase returns the current phase.
func (s *State) Phase() Phase { return s.phase }

// CheckID returns a number that changes whenever a new result replaces the
// issues. Together with an item's ID it identifies the item for caches.
func (s *State) CheckID() uint64 { return s.checkID }

// Revision returns the number of edits seen so far.
func (s *State) Revision() uint64 { return s.revision }

// Items returns every placed issue in text order, whatever its category's
// visibility. The slice is a copy.
func (s *State) Items() []Item { return slices.Clone(s.items) }

// Visible returns the issues whose category is shown, in text order.
func (s *State) Visible() []Item {
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		if !s.hidden[it.Category()] {
			out = append(out, it)
		}
	}
	return out
}

// Get returns the item with the given ID.
func (s *State) Get(id int) (Item, bool) {
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// BeginCheck marks the start of a check on the current text and returns the
// revision the result must be handed back with. The previous check, if still
// pending, is superseded: its result will be treated as stale.
func (s *State) BeginCheck() uint64 {
	if !s.pending {
		s.beforeCheck = s.phase
	}
	s.pending = true
	s.pendingRev = s.revision
	s.log = s.log[:0]
	s.logOverflown = false
	s.setPhase(Checking)
	return s.pendingRev
}

// AbortCheck ends a check that produced no result (cancelled or failed). The
// existing issues stay exactly as they were.
func (s *State) AbortCheck() {
	if !s.pending {
		return
	}
	s.endPending()
	s.setPhase(s.beforeCheck)
}

func (s *State) endPending() {
	s.pending = false
	s.log = s.log[:0]
	s.logOverflown = false
}

func (s *State) setPhase(p Phase) {
	if s.phase == p {
		return
	}
	s.phase = p
	s.notify(Change{Kind: PhaseChanged})
}

// Outcome describes what FinishCheck did with a result.
type Outcome struct {
	// Stale is true if the result belongs to a check that is no longer the
	// pending one; it was ignored.
	Stale bool
	// Discarded is true if the text was edited so much during the check that
	// the result could not be rebased; the old issues are kept.
	Discarded bool
	// Shown is the number of issues in the new result.
	Shown int
	// Dropped is the number of issues of the result that edits made during
	// the check had already invalidated.
	Dropped int
}

// FinishCheck replaces the issues with the result of the check begun at
// revision rev (the value BeginCheck returned). The items' offsets refer to the
// text as it was at rev; edits made since are applied to them first, and items
// the edits touched are dropped. Item IDs are assigned here.
func (s *State) FinishCheck(rev uint64, result []Item) Outcome {
	if !s.pending || rev != s.pendingRev {
		return Outcome{Stale: true}
	}
	overflown := s.logOverflown
	log := slices.Clone(s.log)
	s.endPending()
	if overflown {
		s.setPhase(s.beforeCheck)
		return Outcome{Discarded: true}
	}

	items := slices.Clone(result)
	slices.SortStableFunc(items, func(a, b Item) int { return a.Start - b.Start })
	kept := items[:0]
	dropped := 0
	for _, it := range items {
		r, ok := it.Range(), true
		for _, e := range log {
			if r, ok = r.Rebase(e); !ok {
				break
			}
		}
		if !ok {
			dropped++
			continue
		}
		it.Start, it.End = r.Start, r.End
		kept = append(kept, it)
	}

	oldSelected := s.selected
	removed := make([]int, 0, len(s.items))
	for _, it := range s.items {
		removed = append(removed, it.ID)
	}
	s.items = kept
	for i := range s.items {
		s.items[i].ID = i + 1
	}
	s.selected = 0
	s.checkID++
	s.phase = Checked
	s.notify(Change{Kind: PhaseChanged})
	s.notify(Change{Kind: IssuesChanged, RemovedIDs: removed})
	if oldSelected != 0 {
		s.notify(Change{Kind: SelectionChanged})
	}
	return Outcome{Shown: len(kept), Dropped: dropped}
}

// ApplyEdit records an edit of the text: it moves the issues after it, removes
// the ones it touches, and bumps the revision. While a check is pending the
// edit is also logged so that the result can be rebased.
func (s *State) ApplyEdit(e Edit) {
	s.revision++
	if s.pending {
		if len(s.log) >= maxPendingEdits {
			s.logOverflown = true
		} else {
			s.log = append(s.log, e)
		}
	}

	var removed []int
	changed := false
	kept := s.items[:0]
	for _, it := range s.items {
		r, ok := it.Range().Rebase(e)
		if !ok {
			removed = append(removed, it.ID)
			changed = true
			continue
		}
		if r != it.Range() {
			changed = true
		}
		it.Start, it.End = r.Start, r.End
		kept = append(kept, it)
	}
	s.items = kept
	if !changed {
		return
	}
	selectedGone := slices.Contains(removed, s.selected)
	if selectedGone {
		s.selected = 0
	}
	s.notify(Change{Kind: IssuesChanged, RemovedIDs: removed})
	if selectedGone {
		s.notify(Change{Kind: SelectionChanged})
	}
}

// Reset forgets everything about the previous text: the issues, the selection,
// the phase (back to "not checked") and any pending check. It is for a new or a
// freshly opened text. Category visibility, which the user chose, is kept.
func (s *State) Reset() {
	removed := make([]int, 0, len(s.items))
	for _, it := range s.items {
		removed = append(removed, it.ID)
	}
	hadSelection := s.selected != 0
	s.items = nil
	s.selected = 0
	s.endPending()
	s.checkID++
	s.phase = NotChecked
	s.beforeCheck = NotChecked
	s.notify(Change{Kind: PhaseChanged})
	s.notify(Change{Kind: IssuesChanged, RemovedIDs: removed})
	if hadSelection {
		s.notify(Change{Kind: SelectionChanged})
	}
}

// Remove deletes one issue (the "Not an error" action). It reports whether the
// issue existed.
func (s *State) Remove(id int) bool {
	i := slices.IndexFunc(s.items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		return false
	}
	s.items = slices.Delete(s.items, i, i+1)
	wasSelected := s.selected == id
	if wasSelected {
		s.selected = 0
	}
	s.notify(Change{Kind: IssuesChanged, RemovedIDs: []int{id}})
	if wasSelected {
		s.notify(Change{Kind: SelectionChanged})
	}
	return true
}

// Selected returns the selected issue, if any.
func (s *State) Selected() (Item, bool) {
	if s.selected == 0 {
		return Item{}, false
	}
	return s.Get(s.selected)
}

// Select selects the issue with the given ID. It reports false, and changes
// nothing, if there is no such issue or its category is hidden.
func (s *State) Select(id int) bool {
	it, ok := s.Get(id)
	if !ok || s.hidden[it.Category()] {
		return false
	}
	if s.selected != id {
		s.selected = id
		s.notify(Change{Kind: SelectionChanged})
	}
	return true
}

// ClearSelection deselects the issue, if one is selected.
func (s *State) ClearSelection() {
	if s.selected != 0 {
		s.selected = 0
		s.notify(Change{Kind: SelectionChanged})
	}
}

// SelectAt selects the visible issue that contains the text position pos (an
// issue contains the positions Start up to, not including, End) and clears the
// selection if there is none. It reports whether an issue was selected.
func (s *State) SelectAt(pos int) bool {
	for _, it := range s.Visible() {
		if pos >= it.Start && pos < it.End {
			return s.Select(it.ID)
		}
	}
	s.ClearSelection()
	return false
}

// Next selects the visible issue after the selected one, wrapping around to
// the first. With nothing selected it selects the first issue that starts
// after the cursor position (also wrapping). It returns the issue and false if
// there is no visible issue.
func (s *State) Next(cursor int) (Item, bool) {
	return s.step(cursor, +1)
}

// Prev is like Next, in the other direction: the issue before the selected
// one, or with nothing selected the last one that starts before the cursor.
func (s *State) Prev(cursor int) (Item, bool) {
	return s.step(cursor, -1)
}

func (s *State) step(cursor, dir int) (Item, bool) {
	vis := s.Visible()
	if len(vis) == 0 {
		return Item{}, false
	}
	idx := -1
	if cur, ok := s.Selected(); ok {
		idx = slices.IndexFunc(vis, func(it Item) bool { return it.ID == cur.ID })
	}
	var target int
	switch {
	case idx >= 0:
		target = (idx + dir + len(vis)) % len(vis)
	case dir > 0:
		target = 0 // wraps to the first unless one starts after the cursor
		for i, it := range vis {
			if it.Start > cursor {
				target = i
				break
			}
		}
	default:
		target = len(vis) - 1 // wraps to the last unless one starts before the cursor
		for i := len(vis) - 1; i >= 0; i-- {
			if vis[i].Start < cursor {
				target = i
				break
			}
		}
	}
	s.Select(vis[target].ID)
	return vis[target], true
}

// CategoryVisible reports whether underlines and rows of the category are shown.
func (s *State) CategoryVisible(c analysis.Category) bool { return !s.hidden[c] }

// SetCategoryVisible shows or hides a category. Hiding the category of the
// selected issue clears the selection.
func (s *State) SetCategoryVisible(c analysis.Category, visible bool) {
	if s.hidden[c] == !visible {
		return
	}
	if visible {
		delete(s.hidden, c)
	} else {
		s.hidden[c] = true
	}
	s.notify(Change{Kind: VisibilityChanged})
	if cur, ok := s.Selected(); ok && s.hidden[cur.Category()] {
		s.ClearSelection()
	}
}
