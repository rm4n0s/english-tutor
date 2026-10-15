package issues

import (
	"math/rand/v2"
	"testing"
)

// doc is a model of the text: every unit has a unique identity, so after any
// sequence of edits we can tell exactly which units an issue still covers.
type doc struct {
	ids  []int
	next int
}

func newDoc(n int) *doc {
	d := &doc{}
	for range n {
		d.ids = append(d.ids, d.next)
		d.next++
	}
	return d
}

func (d *doc) apply(e Edit) {
	added := make([]int, e.Added)
	for i := range added {
		added[i] = d.next
		d.next++
	}
	out := append([]int(nil), d.ids[:e.Pos]...)
	out = append(out, added...)
	out = append(out, d.ids[e.Pos+e.Removed:]...)
	d.ids = out
}

func (d *doc) randomEdit(r *rand.Rand) Edit {
	pos := r.IntN(len(d.ids) + 1)
	removed := 0
	if len(d.ids)-pos > 0 && r.IntN(2) == 0 {
		removed = r.IntN(min(6, len(d.ids)-pos) + 1)
	}
	added := 0
	if r.IntN(3) > 0 {
		added = r.IntN(6)
	}
	return Edit{Pos: pos, Removed: removed, Added: added}
}

// randomItems places up to n non-overlapping issues in the document.
func (d *doc) randomItems(r *rand.Rand, n int) []Item {
	var items []Item
	pos := 0
	for range n {
		pos += r.IntN(8)
		length := 1 + r.IntN(6)
		if pos+length > len(d.ids) {
			break
		}
		items = append(items, item("article", pos, pos+length))
		pos += length
	}
	return items
}

func (d *doc) identity(r Range) []int { return append([]int(nil), d.ids[r.Start:r.End]...) }

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runModel applies random edits both to the model document and to the State
// (directly and through a pending check) and checks the invariants:
//
//   - every issue the State still holds covers exactly the units it covered
//     when it was placed (so its underline is still on the same words);
//   - issues stay sorted and do not overlap;
//   - an issue that no edit came near is never dropped.
func runModel(t *testing.T, seed1, seed2 uint64, steps int) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed1, seed2))
	d := newDoc(40 + r.IntN(60))
	s := New()
	rev := s.BeginCheck()
	items := d.randomItems(r, 8)
	identities := map[int][]int{} // item start offset at rev -> identity
	for _, it := range items {
		identities[it.Start] = d.identity(it.Range())
	}
	s.FinishCheck(rev, items)
	want := map[int][]int{} // item ID -> identity
	for _, it := range s.Items() {
		want[it.ID] = identities[it.Start]
	}

	for step := range steps {
		e := d.randomEdit(r)

		// Items that the edit is far from must survive it.
		mustSurvive := map[int]bool{}
		for _, it := range s.Items() {
			if e.Pos+e.Removed < it.Start || e.Pos > it.End {
				mustSurvive[it.ID] = true
			}
		}

		d.apply(e)
		s.ApplyEdit(e)

		lastEnd := 0
		for _, it := range s.Items() {
			if it.Start < lastEnd || it.End <= it.Start || it.End > len(d.ids) {
				t.Fatalf("step %d: bad range [%d,%d) after %d (doc has %d units)", step, it.Start, it.End, lastEnd, len(d.ids))
			}
			lastEnd = it.End
			if !sameInts(d.identity(it.Range()), want[it.ID]) {
				t.Fatalf("step %d: issue %d no longer covers its original units after %+v", step, it.ID, e)
			}
			delete(mustSurvive, it.ID)
		}
		if len(mustSurvive) > 0 {
			t.Fatalf("step %d: issues %v were dropped by %+v although it was not near them", step, mustSurvive, e)
		}
	}
}

func TestModelRandomEdits(t *testing.T) {
	for seed := range uint64(300) {
		runModel(t, seed, seed*7+1, 60)
	}
}

// TestModelRebaseAcrossAPendingCheck checks the same invariant for a result
// that arrives after the text changed: the document is edited while the check
// runs, then the result computed on the old text is rebased.
func TestModelRebaseAcrossAPendingCheck(t *testing.T) {
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 99))
		d := newDoc(40 + r.IntN(60))
		s := New()
		rev := s.BeginCheck()
		items := d.randomItems(r, 8)
		identity := map[int][]int{}
		for _, it := range items {
			identity[it.Start] = d.identity(it.Range())
		}

		for range 1 + r.IntN(25) {
			e := d.randomEdit(r)
			d.apply(e)
			s.ApplyEdit(e)
		}
		out := s.FinishCheck(rev, items)
		if out.Stale || out.Discarded || out.Shown+out.Dropped != len(items) {
			t.Fatalf("seed %d: outcome %+v for %d items", seed, out, len(items))
		}
		// Match survivors to the original items by identity.
		for _, it := range s.Items() {
			got := d.identity(it.Range())
			found := false
			for _, orig := range identity {
				if sameInts(got, orig) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("seed %d: issue [%d,%d) covers units that no original issue covered", seed, it.Start, it.End)
			}
		}
	}
}

func FuzzModelEdits(f *testing.F) {
	f.Add(uint64(1), uint64(2), 30)
	f.Add(uint64(0), uint64(0), 5)
	f.Fuzz(func(t *testing.T, a, b uint64, steps int) {
		runModel(t, a, b, steps%100)
	})
}
