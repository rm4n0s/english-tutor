package analysis

import (
	"strings"
	"testing"
)

func fp(text, snippet string, nth int) string {
	// The start of the nth (0-based) occurrence of snippet.
	pos := -1
	for i := 0; i <= nth; i++ {
		next := strings.Index(text[pos+1:], snippet)
		if next < 0 {
			panic("snippet not found")
		}
		pos += 1 + next
	}
	return Fingerprint(text, pos, pos+len(snippet))
}

func TestNormaliseSnippet(t *testing.T) {
	tests := []struct{ in, want string }{
		{"He Go", "he go"},
		{"  he \n go\t", "he go"},
		{"ÉCOLE", "école"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormaliseSnippet(tt.in); got != tt.want {
			t.Errorf("NormaliseSnippet(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	text := "He go home. She say hi. He go out."
	first := fp(text, "He go", 0)
	second := fp(text, "He go", 1)

	t.Run("same mistake in the same text is stable", func(t *testing.T) {
		if first != fp(text, "He go", 0) {
			t.Error("fingerprint changed between calls")
		}
	})
	t.Run("two occurrences of one snippet differ", func(t *testing.T) {
		if first == second {
			t.Error("the second occurrence must have its own fingerprint")
		}
	})
	t.Run("different snippets differ", func(t *testing.T) {
		if first == fp(text, "She say", 0) {
			t.Error("different snippets share a fingerprint")
		}
	})
	t.Run("case and spacing of the snippet do not matter", func(t *testing.T) {
		a := Fingerprint("he  go home", 0, len("he  go"))
		b := Fingerprint("HE GO home", 0, len("HE GO"))
		if a != b {
			t.Error("normalisation was not applied")
		}
	})
	t.Run("text after the snippet does not matter", func(t *testing.T) {
		a := Fingerprint("He go home.", 0, 5)
		b := Fingerprint("He go somewhere else entirely.", 0, 5)
		if a != b {
			t.Error("a later edit must not change the fingerprint")
		}
	})
	t.Run("an unrelated edit before does not matter", func(t *testing.T) {
		a := fp("Hello. He go home.", "He go", 0)
		b := fp("A much longer start of the text. He go home.", "He go", 0)
		if a != b {
			t.Error("text before that does not contain the snippet must not change the fingerprint")
		}
	})
	t.Run("an earlier identical snippet shifts the later indexes (known limit)", func(t *testing.T) {
		old := fp("He go home.", "He go", 0)
		edited := "He go there. He go home."
		if fp(edited, "He go", 0) != old {
			t.Error("the first occurrence keeps index 0")
		}
		if fp(edited, "He go", 1) == old {
			t.Error("the old mistake is now occurrence 1 and so has a different fingerprint; the spec accepts this limit")
		}
	})
	t.Run("hex sha-256", func(t *testing.T) {
		if len(first) != 64 || strings.Trim(first, "0123456789abcdef") != "" {
			t.Errorf("fingerprint = %q", first)
		}
	})
	t.Run("empty snippet does not panic", func(t *testing.T) {
		_ = Fingerprint("abc", 1, 1)
	})
	t.Run("occurrence ignores case", func(t *testing.T) {
		upper := Fingerprint("HE GO. he go", 7, 12)
		lower := Fingerprint("he go. he go", 7, 12)
		if upper != lower {
			t.Error("occurrence count must ignore case")
		}
	})
}
