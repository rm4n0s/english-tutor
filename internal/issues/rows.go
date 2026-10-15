package issues

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// SnippetRunes is the longest snippet shown in a list row.
const SnippetRunes = 40

// Row is what the issues list shows for one issue (section 4.3).
type Row struct {
	ID       int
	Category analysis.Category
	// Marker is the letter of the category (G, V or S), so that the category is
	// not shown by colour alone.
	Marker string
	// Snippet is the wrong text on one line, shortened.
	Snippet string
	// Label is the error type's name, shown on the second line.
	Label string
	// Count is how many times the user has made this error type; the badge is
	// hidden when it is not positive.
	Count int
}

// Badge returns the count badge ("x7"), or "" when there is nothing to show.
func (r Row) Badge() string {
	if r.Count <= 0 {
		return ""
	}
	return fmt.Sprintf("x%d", r.Count)
}

// RowFor builds the row of an item. count is the number of mistakes of the
// item's type recorded so far (0 if the issue is not recorded).
func RowFor(it Item, count int) Row {
	return Row{
		ID:       it.ID,
		Category: it.Category(),
		Marker:   categoryMarker(it.Category()),
		Snippet:  ShortenSnippet(it.Text, SnippetRunes),
		Label:    it.Type.Label,
		Count:    count,
	}
}

func categoryMarker(c analysis.Category) string {
	switch c {
	case analysis.Grammar:
		return "G"
	case analysis.Vocabulary:
		return "V"
	case analysis.Syntax:
		return "S"
	default:
		return "?"
	}
}

// ShortenSnippet puts s on one line (every run of whitespace becomes one
// space) and cuts it to at most max runes, ending with "…" when it was cut.
func ShortenSnippet(s string, max int) string {
	s = strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return strings.TrimRightFunc(string([]rune(s)[:max-1]), unicode.IsSpace) + "…"
}

// Header returns the title above the list: "Issues (5)", or "Issues (3 of 5
// shown)" while some categories are hidden.
func (s *State) Header() string {
	shown, total := len(s.Visible()), len(s.items)
	if shown == total {
		return fmt.Sprintf("Issues (%d)", total)
	}
	return fmt.Sprintf("Issues (%d of %d shown)", shown, total)
}

// EmptyText returns the grey line shown in place of rows, or "" when there
// are rows to show.
func (s *State) EmptyText() string {
	switch {
	case s.phase == Checking && len(s.items) == 0:
		return "Checking..."
	case len(s.Visible()) > 0:
		return ""
	case len(s.items) > 0:
		return "All categories are hidden"
	case s.phase == Checked:
		return "No errors found"
	default:
		return "Press Check to find errors"
	}
}
