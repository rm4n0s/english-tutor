package gui

import (
	"testing"
	"unicode/utf16"
)

func units(s string) []uint16 { return utf16.Encode([]rune(s)) }

// apply performs e on old, taking the added units from updated.
func apply(old, updated string, e Edit) []uint16 {
	o, n := units(old), units(updated)
	out := append([]uint16(nil), o[:e.Pos]...)
	out = append(out, n[e.Pos:e.Pos+e.Added]...)
	return append(out, o[e.Pos+e.Removed:]...)
}

func equal(a, b []uint16) bool {
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

func TestDiffText(t *testing.T) {
	tests := []struct {
		name         string
		old, updated string
		caret        int
		want         Edit
	}{
		{"no change", "abc", "abc", 3, Edit{}},
		{"type at the end", "abc", "abcd", 4, Edit{3, 0, 1}},
		{"type at the start", "abc", "xabc", 1, Edit{0, 0, 1}},
		{"type in the middle", "abc", "abxc", 3, Edit{2, 0, 1}},
		{"repeated letter is placed by the caret, first", "aa", "aaa", 1, Edit{0, 0, 1}},
		{"repeated letter is placed by the caret, last", "aa", "aaa", 3, Edit{2, 0, 1}},
		{"backspace", "abc", "ab", 2, Edit{2, 1, 0}},
		{"delete forward", "abc", "ac", 1, Edit{1, 1, 0}},
		{"repeated letter deleted by backspace", "aaa", "aa", 1, Edit{1, 1, 0}},
		{"select all and type", "abc", "x", 1, Edit{0, 3, 1}},
		{"replace a word", "a big dog", "a small dog", 7, Edit{2, 3, 5}},
		{"emoji typed", "ab", "a😀b", 5, Edit{1, 0, 2}},
		{"emoji deleted", "a😀b", "ab", 1, Edit{1, 2, 0}},
		{"accent replaced", "café", "cafe", 4, Edit{3, 1, 1}},
		{"two emoji, one replaced", "😀😁", "😀😂", 8, Edit{2, 2, 2}},
		{"paste over a selection", "hello world", "hello there world", 12, Edit{6, 0, 6}},
		{"no caret", "abc", "abcd", -1, Edit{3, 0, 1}},
		{"to empty", "abc", "", 0, Edit{0, 3, 0}},
		{"from empty", "", "abc", 3, Edit{0, 0, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffText(tt.old, tt.updated, tt.caret)
			if got != tt.want {
				t.Errorf("DiffText(%q, %q, %d) = %+v, want %+v", tt.old, tt.updated, tt.caret, got, tt.want)
			}
			if !equal(apply(tt.old, tt.updated, got), units(tt.updated)) {
				t.Errorf("applying %+v to %q does not give %q", got, tt.old, tt.updated)
			}
		})
	}
}

// FuzzDiffText checks that the edit always turns the old text into the new one,
// whatever the texts and the caret are.
func FuzzDiffText(f *testing.F) {
	for _, s := range [][3]any{{"abc", "abcd", 4}, {"aa", "aaa", 1}, {"a😀b", "ab", 1}, {"", "é", 2}, {"\xff", "\xfe", 0}} {
		f.Add(s[0].(string), s[1].(string), s[2].(int))
	}
	f.Fuzz(func(t *testing.T, old, updated string, caret int) {
		e := DiffText(old, updated, caret)
		// Invalid UTF-8 is counted unit by unit on both sides, so only valid texts
		// can be replayed on the units.
		if !utf16Valid(old) || !utf16Valid(updated) {
			return
		}
		if e.Pos < 0 || e.Removed < 0 || e.Added < 0 {
			t.Fatalf("negative edit %+v", e)
		}
		if !equal(apply(old, updated, e), units(updated)) {
			t.Errorf("edit %+v of %q to %q does not replay", e, old, updated)
		}
	})
}

func utf16Valid(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}
