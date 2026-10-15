package analysis

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// refUnits is the reference implementation: the UTF-16 length of text[:b] where
// b is on a rune boundary.
func refUnits(text string, b int) int {
	return len(utf16.Encode([]rune(text[:b])))
}

func TestUTF16MapKnownValues(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		bytes []int
		units []int
	}{
		{"ascii", "hello", []int{0, 2, 5}, []int{0, 2, 5}},
		{"accented letters are 2 bytes, 1 unit", "café au", []int{0, 3, 5, 7}, []int{0, 3, 4, 6}},
		{"cjk is 3 bytes, 1 unit", "日本語", []int{0, 3, 6, 9}, []int{0, 1, 2, 3}},
		{"emoji is 4 bytes, 2 units", "a😀b", []int{0, 1, 5, 6}, []int{0, 1, 3, 4}},
		{"two emoji", "😀😀x", []int{0, 4, 8, 9}, []int{0, 2, 4, 5}},
		{"empty", "", []int{0}, []int{0}},
		{"combining accent counts as its own unit", "é", []int{0, 1, 3}, []int{0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewUTF16Map(tt.text)
			for i, b := range tt.bytes {
				if got := m.Offset(b); got != tt.units[i] {
					t.Errorf("Offset(%d) = %d, want %d", b, got, tt.units[i])
				}
				if got := m.ByteOffset(tt.units[i]); got != b {
					t.Errorf("ByteOffset(%d) = %d, want %d", tt.units[i], got, b)
				}
			}
			if got, want := m.Units(), len(utf16.Encode([]rune(tt.text))); got != want {
				t.Errorf("Units() = %d, want %d", got, want)
			}
		})
	}
}

func TestUTF16MapMidRuneAndClamping(t *testing.T) {
	m := NewUTF16Map("a😀b") // bytes: a=0, 😀=1..4, b=5
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"byte inside the emoji maps to its start", m.Offset(3), 1},
		{"unit inside the surrogate pair maps to the emoji start", m.ByteOffset(2), 1},
		{"negative byte offset", m.Offset(-5), 0},
		{"byte offset past the end", m.Offset(99), 4},
		{"negative unit offset", m.ByteOffset(-5), 0},
		{"unit offset past the end", m.ByteOffset(99), 6},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestUTF16MapInvalidUTF8CountsOneUnitPerByte(t *testing.T) {
	m := NewUTF16Map("a\xff\xfeb")
	if got := m.Units(); got != 4 {
		t.Errorf("Units() = %d, want 4", got)
	}
	if got := m.Offset(3); got != 3 {
		t.Errorf("Offset(3) = %d, want 3", got)
	}
}

func TestUTF16MapLongTextUsesCheckpoints(t *testing.T) {
	// Mixed widths well past several checkpoints, including runes that
	// straddle a checkpoint boundary.
	text := strings.Repeat("ab😀é日 ", 700)
	m := NewUTF16Map(text)
	if len(m.marks) < 3 {
		t.Fatalf("expected several checkpoints, got %d", len(m.marks))
	}
	for b := 0; b <= len(text); b++ {
		if b < len(text) && !utf8.RuneStart(text[b]) {
			continue
		}
		want := refUnits(text, b)
		if got := m.Offset(b); got != want {
			t.Fatalf("Offset(%d) = %d, want %d", b, got, want)
		}
		if got := m.ByteOffset(want); got != b {
			t.Fatalf("ByteOffset(%d) = %d, want %d", want, got, b)
		}
	}
}

func FuzzUTF16Map(f *testing.F) {
	for _, s := range []string{"", "abc", "café", "a😀b", "\xff\xfe", strings.Repeat("😀é", 1500)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		m := NewUTF16Map(text)
		if !utf8.ValidString(text) {
			// Only check that nothing panics and the offsets are monotonic.
			prev := -1
			for b := 0; b <= len(text); b++ {
				got := m.Offset(b)
				if got < prev {
					t.Fatalf("Offset not monotonic at %d", b)
				}
				prev = got
			}
			return
		}
		for b := 0; b <= len(text); b++ {
			if b < len(text) && !utf8.RuneStart(text[b]) {
				continue
			}
			u := m.Offset(b)
			if want := refUnits(text, b); u != want {
				t.Fatalf("Offset(%d) = %d, want %d", b, u, want)
			}
			if back := m.ByteOffset(u); back != b {
				t.Fatalf("ByteOffset(Offset(%d)) = %d", b, back)
			}
		}
	})
}
