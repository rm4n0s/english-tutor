package analysis

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func issue(text string) Issue { return Issue{Text: text, Type: Resolve("article")} }

func withContext(text, before, after string) Issue {
	i := issue(text)
	i.ContextBefore, i.ContextAfter = before, after
	return i
}

func TestLocate(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		issues     []Issue
		wantSpans  [][2]int // start, end of each placed issue, in order
		unplaced   int
		overlapped int
	}{
		{
			name:      "single occurrence",
			text:      "Yesterday he go to school.",
			issues:    []Issue{issue("he go")},
			wantSpans: [][2]int{{10, 15}},
		},
		{
			name:   "context picks the second of two",
			text:   "I has a cat. You has a dog.",
			issues: []Issue{withContext("has", "You ", " a dog")},
			// "You has": 'has' starts at 17
			wantSpans: [][2]int{{17, 20}},
		},
		{
			name:      "context picks the first of two",
			text:      "I has a cat. You has a dog.",
			issues:    []Issue{withContext("has", "", " a cat")},
			wantSpans: [][2]int{{2, 5}},
		},
		{
			name:      "context with trimmed whitespace still matches",
			text:      "I has a cat. You has a dog.",
			issues:    []Issue{withContext("has", "You", "a dog.")},
			wantSpans: [][2]int{{17, 20}},
		},
		{
			name:   "tie goes to first unclaimed, one issue per occurrence",
			text:   "teh cat and teh dog",
			issues: []Issue{issue("teh"), issue("teh")},
			wantSpans: [][2]int{
				{0, 3}, {12, 15},
			},
		},
		{
			name:       "more reports than occurrences: the extra overlaps",
			text:       "teh cat and teh dog",
			issues:     []Issue{issue("teh"), issue("teh"), issue("teh")},
			wantSpans:  [][2]int{{0, 3}, {12, 15}},
			overlapped: 1,
		},
		{
			name:      "word boundary beats a match inside another word",
			text:      "good go",
			issues:    []Issue{issue("go")},
			wantSpans: [][2]int{{5, 7}},
		},
		{
			name:      "single occurrence inside a word is still used",
			text:      "good",
			issues:    []Issue{issue("go")},
			wantSpans: [][2]int{{0, 2}},
		},
		{
			name:      "case-insensitive fallback",
			text:      "He GO to school",
			issues:    []Issue{issue("he go")},
			wantSpans: [][2]int{{0, 5}},
		},
		{
			name:      "whitespace-normalised fallback with newline",
			text:      "He go\nto school",
			issues:    []Issue{issue("go to")},
			wantSpans: [][2]int{{3, 8}},
		},
		{
			name:      "whitespace-normalised fallback with several spaces",
			text:      "He   go    to school",
			issues:    []Issue{issue("He go to")},
			wantSpans: [][2]int{{0, 13}},
		},
		{
			name:      "curly apostrophe matches a straight one",
			text:      "I don’t know nothing.",
			issues:    []Issue{issue("don't know")},
			wantSpans: [][2]int{{2, 14}},
		},
		{
			name:      "dash and quote folding",
			text:      "She said “hello” — twice.",
			issues:    []Issue{issue(`"hello" - twice`)},
			wantSpans: [][2]int{{9, 30}},
		},
		{
			name:      "fallback maps multi-byte text back correctly",
			text:      "Café  AU lait",
			issues:    []Issue{issue("café au")},
			wantSpans: [][2]int{{0, 9}},
		},
		{
			name:     "not in the text",
			text:     "Nothing to see.",
			issues:   []Issue{issue("something else")},
			unplaced: 1,
		},
		{
			name:     "invalid utf-8 snippet never matches inside a character (found by fuzzing)",
			text:     "\u013f",
			issues:   []Issue{issue("\xbf")},
			unplaced: 1,
		},
		{
			name:     "empty snippet",
			text:     "Some text.",
			issues:   []Issue{issue(""), issue("   ")},
			unplaced: 2,
		},
		{
			name:     "empty text",
			text:     "",
			issues:   []Issue{issue("a")},
			unplaced: 1,
		},
		{
			name:       "overlap keeps the earlier one in the text",
			text:       "He go to the school now",
			issues:     []Issue{issue("go to the school"), issue("He go")},
			wantSpans:  [][2]int{{0, 5}},
			overlapped: 1,
		},
		{
			name:      "touching spans do not overlap",
			text:      "abcd",
			issues:    []Issue{issue("cd"), issue("ab")},
			wantSpans: [][2]int{{0, 2}, {2, 4}},
		},
		{
			name:       "same span twice",
			text:       "He go home",
			issues:     []Issue{issue("go"), issue("go")},
			wantSpans:  [][2]int{{3, 5}},
			overlapped: 1,
		},
		{
			name:      "result is sorted by position",
			text:      "one two three",
			issues:    []Issue{issue("three"), issue("one"), issue("two")},
			wantSpans: [][2]int{{0, 3}, {4, 7}, {8, 13}},
		},
		{
			name:      "emoji before the snippet",
			text:      "😀😀 he go",
			issues:    []Issue{issue("he go")},
			wantSpans: [][2]int{{9, 14}},
		},
		{
			name:      "overlapping occurrences of a repeating snippet",
			text:      "aaa",
			issues:    []Issue{withContext("aa", "a", "")},
			wantSpans: [][2]int{{1, 3}},
		},
		{
			name:      "snippet is trimmed",
			text:      "He go home",
			issues:    []Issue{issue("  go  ")},
			wantSpans: [][2]int{{3, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Locate(tt.text, tt.issues)
			if got.Unplaced != tt.unplaced || got.Overlapping != tt.overlapped {
				t.Errorf("unplaced/overlapping = %d/%d, want %d/%d", got.Unplaced, got.Overlapping, tt.unplaced, tt.overlapped)
			}
			if len(got.Placed) != len(tt.wantSpans) {
				t.Fatalf("placed = %+v, want spans %v", got.Placed, tt.wantSpans)
			}
			for i, p := range got.Placed {
				if p.Start != tt.wantSpans[i][0] || p.End != tt.wantSpans[i][1] {
					t.Errorf("placed[%d] = [%d,%d) %q, want %v", i, p.Start, p.End, tt.text[p.Start:p.End], tt.wantSpans[i])
				}
			}
		})
	}
}

func TestLocateKeepsTheIssue(t *testing.T) {
	is := Issue{Text: "go", Type: Resolve("verb_form"), Message: "msg"}
	got := Locate("He go", []Issue{is})
	if len(got.Placed) != 1 || got.Placed[0].Issue != is {
		t.Errorf("Locate() = %+v", got)
	}
}

func TestLocateAppend(t *testing.T) {
	var all LocateResult
	all.Append(Locate("He go", []Issue{issue("go"), issue("zzz")}), 0)
	all.Append(Locate("She go", []Issue{issue("go"), withContext("She", "", "")}), 100)
	if len(all.Placed) != 3 || all.Unplaced != 1 || all.Overlapping != 0 {
		t.Fatalf("Append result = %+v", all)
	}
	wantStarts := []int{3, 100, 104} // the second chunk is sorted: "She" then "go", shifted by 100
	for i, p := range all.Placed {
		if p.Start != wantStarts[i] {
			t.Errorf("placed[%d].Start = %d, want %d", i, p.Start, wantStarts[i])
		}
	}
}

func FuzzLocate(f *testing.F) {
	f.Add("He go to school.", "go", "He ", " to")
	f.Add("😀 café  AU lait", "café au", "", "")
	f.Add("aaa", "aa", "a", "")
	f.Add("", "x", "", "")
	f.Fuzz(func(t *testing.T, text, snippet, before, after string) {
		issues := []Issue{
			{Text: snippet, ContextBefore: before, ContextAfter: after},
			{Text: snippet},
			{Text: strings.ToUpper(snippet)},
		}
		res := Locate(text, issues)
		if res.Unplaced+res.Overlapping+len(res.Placed) != len(issues) {
			t.Fatalf("counts do not add up: %+v", res)
		}
		last := 0
		for _, p := range res.Placed {
			if p.Start < last || p.End <= p.Start || p.End > len(text) {
				t.Fatalf("bad span [%d,%d) after %d in text of %d bytes", p.Start, p.End, last, len(text))
			}
			if utf8.ValidString(text) && (!utf8.RuneStart(text[p.Start]) || (p.End < len(text) && !utf8.RuneStart(text[p.End]))) {
				t.Fatalf("span [%d,%d) is not on rune boundaries in %q", p.Start, p.End, text)
			}
			last = p.End
		}
	})
}
