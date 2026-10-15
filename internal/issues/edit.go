// Package issues holds the state of the issues found by the last check: the
// placed issues, the shared selection, which categories are visible, and how
// the issues follow edits of the text. It is plain Go with no Qt imports; the
// editor and the issues list both observe one State and never talk to each
// other (section 4.3 of the spec).
//
// All offsets in this package are UTF-16 code units, the unit Qt uses for text
// positions. Use FromPlaced to convert the byte offsets of the analysis
// package.
package issues

// Edit describes one change to the text, as reported by QTextDocument's
// contentsChange signal: at position Pos, Removed units were replaced by Added
// units.
type Edit struct {
	Pos     int
	Removed int
	Added   int
}

// Range is a half-open range [Start, End) of UTF-16 units.
type Range struct {
	Start int
	End   int
}

// Rebase moves r across e. It reports false if the edit touches the range, in
// which case the issue no longer describes the text and must go away.
//
//   - An edit that ends before the range starts shifts it by the net change.
//   - An edit that starts after the range ends leaves it alone.
//   - Anything else touches the range: it overlaps it, or is adjacent to it
//     (typing right after a word changes that word).
func (r Range) Rebase(e Edit) (Range, bool) {
	switch {
	case e.Pos+e.Removed < r.Start:
		delta := e.Added - e.Removed
		return Range{r.Start + delta, r.End + delta}, true
	case e.Pos > r.End:
		return r, true
	default:
		return Range{}, false
	}
}
