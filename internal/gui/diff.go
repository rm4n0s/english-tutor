package gui

import (
	"unicode/utf8"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// Edit is one change of the text, in UTF-16 units: Removed units at Pos were
// replaced by Added units. It is what Qt's contentsChange reported and what the
// issue state rebases its issues on.
type Edit struct{ Pos, Removed, Added int }

// DiffText finds the edit that turns old into updated. The text field only
// reports the new text, so the edit is recovered from the two texts. caret is the
// byte offset of the caret in updated, or -1; it settles where the change is
// when that is ambiguous (typing "a" into "aa" could be at either end), so that
// issues are moved the way the user's edit moved them.
func DiffText(old, updated string, caret int) Edit {
	if old == updated {
		return Edit{}
	}
	prefix := commonPrefix(old, updated)
	suffix := commonSuffix(old, updated)
	pos, removed, added := 0, 0, 0
	switch {
	case len(updated) > len(old) && prefix+suffix >= len(old): // only an insertion
		added = len(updated) - len(old)
		pos = prefix
		if caret >= 0 {
			pos = min(max(caret-added, len(old)-suffix), prefix)
		}
	case len(updated) < len(old) && prefix+suffix >= len(updated): // only a deletion
		removed = len(old) - len(updated)
		pos = prefix
		if caret >= 0 {
			pos = min(max(caret, len(updated)-suffix), prefix)
		}
	default:
		pos = prefix
		suffix = min(suffix, min(len(old), len(updated))-prefix)
		removed = len(old) - prefix - suffix
		added = len(updated) - prefix - suffix
	}
	pos = runeStart(old, pos)
	oldMap, newMap := analysis.NewUTF16Map(old), analysis.NewUTF16Map(updated)
	e := Edit{Pos: oldMap.Offset(pos)}
	e.Removed = oldMap.Offset(pos+removed) - e.Pos
	e.Added = newMap.Offset(pos+added) - newMap.Offset(pos)
	return e
}

// commonPrefix is the length in bytes of the longest prefix the texts share
// that ends at a character boundary of both.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	for n > 0 && (n < len(a) && !utf8.RuneStart(a[n]) || n < len(b) && !utf8.RuneStart(b[n])) {
		n--
	}
	return n
}

// commonSuffix is the length in bytes of the longest suffix the texts share that
// starts at a character boundary of both.
func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	for n > 0 && (!utf8.RuneStart(a[len(a)-n]) || !utf8.RuneStart(b[len(b)-n])) {
		n--
	}
	return n
}

// runeStart moves pos back to the start of the character it is inside.
func runeStart(s string, pos int) int {
	pos = min(max(pos, 0), len(s))
	for pos > 0 && pos < len(s) && !utf8.RuneStart(s[pos]) {
		pos--
	}
	return pos
}
