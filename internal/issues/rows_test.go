package issues

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

func TestShortenSnippet(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"short stays", "he go", 40, "he go"},
		{"exactly at the limit stays", strings.Repeat("a", 40), 40, strings.Repeat("a", 40)},
		{"one over is cut with an ellipsis", strings.Repeat("a", 41), 40, strings.Repeat("a", 39) + "…"},
		{"newlines and runs of spaces become one space", "he\n  go\t\tto", 40, "he go to"},
		{"leading and trailing space is dropped", "  he go  ", 40, "he go"},
		{"cut never leaves a space before the ellipsis", "aaaa bbbb", 6, "aaaa…"},
		{"counts runes not bytes", strings.Repeat("é", 10), 5, "éééé…"},
		{"emoji are not split", strings.Repeat("😀", 10), 4, "😀😀😀…"},
		{"empty", "", 40, ""},
		{"tiny limit", "abc", 1, "…"},
		{"zero limit", "abc", 0, "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShortenSnippet(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("ShortenSnippet(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Error("result is not valid UTF-8")
			}
		})
	}
}

func TestRowFor(t *testing.T) {
	tests := []struct {
		name       string
		typeID     string
		count      int
		wantMarker string
		wantBadge  string
		wantCat    analysis.Category
	}{
		{"grammar with a count", "subject_verb_agreement", 7, "G", "x7", analysis.Grammar},
		{"vocabulary first time", "spelling", 1, "V", "x1", analysis.Vocabulary},
		{"syntax not recorded", "word_order", 0, "S", "", analysis.Syntax},
		{"negative count hides the badge", "article", -3, "G", "", analysis.Grammar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := item(tt.typeID, 0, 5)
			it.ID = 4
			it.Text = "he\ngo  home"
			row := RowFor(it, tt.count)
			if row.ID != 4 || row.Marker != tt.wantMarker || row.Badge() != tt.wantBadge || row.Category != tt.wantCat {
				t.Errorf("row = %+v, badge %q", row, row.Badge())
			}
			if row.Snippet != "he go home" {
				t.Errorf("snippet = %q", row.Snippet)
			}
			if row.Label != analysis.Resolve(tt.typeID).Label {
				t.Errorf("label = %q", row.Label)
			}
		})
	}
}

func TestMarkersAreDistinct(t *testing.T) {
	seen := map[string]analysis.Category{}
	for _, c := range analysis.Categories() {
		m := categoryMarker(c)
		if m == "?" {
			t.Errorf("category %s has no marker", c)
		}
		if other, dup := seen[m]; dup {
			t.Errorf("categories %s and %s share the marker %s", other, c, m)
		}
		seen[m] = c
	}
}
