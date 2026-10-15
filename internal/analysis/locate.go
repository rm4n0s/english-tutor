package analysis

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Placed is an issue found in the text. Start and End are byte offsets into
// the text that was searched, with End exclusive; both are on rune boundaries.
type Placed struct {
	Issue Issue
	Start int
	End   int
}

// LocateResult is the outcome of placing a list of issues in a text.
type LocateResult struct {
	// Placed is sorted by Start and never overlaps.
	Placed []Placed
	// Unplaced counts issues whose snippet is not in the text at all.
	Unplaced int
	// Overlapping counts issues dropped because they overlap an earlier one
	// (or report the same span twice).
	Overlapping int
}

// Append adds the placed issues of o, with their offsets moved by offset bytes
// (the start of the chunk o was located in), and adds up the counters. Chunks
// are disjoint, so the result stays sorted and free of overlaps when o's
// chunk comes after the chunks already appended.
func (r *LocateResult) Append(o LocateResult, offset int) {
	for _, p := range o.Placed {
		p.Start += offset
		p.End += offset
		r.Placed = append(r.Placed, p)
	}
	r.Unplaced += o.Unplaced
	r.Overlapping += o.Overlapping
}

type span struct{ start, end int }

// Locate finds each issue's snippet in text (section 5.3 of the spec):
//
//  1. A snippet that occurs once is used.
//  2. If it occurs several times, the occurrence on word boundaries whose
//     neighbouring text best matches the issue's context wins; remaining ties
//     go to the first occurrence not already taken by an earlier issue.
//  3. If it does not occur, a case-insensitive match that ignores differences
//     in whitespace and typographic quotes and dashes is tried.
//  4. Issues that cannot be found are counted in Unplaced; issues that overlap
//     an earlier one in the text are dropped and counted in Overlapping.
func Locate(text string, issues []Issue) LocateResult {
	var (
		res     LocateResult
		placed  []Placed
		claimed = map[span]bool{}
		folded  *foldedText // built on first use
	)
	for _, is := range issues {
		sp, ok := findIssue(text, is, claimed, &folded)
		if !ok {
			res.Unplaced++
			continue
		}
		claimed[sp] = true
		placed = append(placed, Placed{Issue: is, Start: sp.start, End: sp.end})
	}

	// Stable, so two issues starting at the same place keep the model's order.
	sort.SliceStable(placed, func(i, j int) bool { return placed[i].Start < placed[j].Start })
	lastEnd := 0
	for _, p := range placed {
		if p.Start < lastEnd {
			res.Overlapping++
			continue
		}
		res.Placed = append(res.Placed, p)
		lastEnd = p.End
	}
	return res
}

func findIssue(text string, is Issue, claimed map[span]bool, folded **foldedText) (span, bool) {
	snippet := strings.TrimSpace(is.Text)
	if snippet == "" {
		return span{}, false
	}
	cands := occurrences(text, snippet)
	if len(cands) == 0 {
		if *folded == nil {
			*folded = foldText(text)
		}
		cands = (*folded).occurrences(snippet)
	}
	switch len(cands) {
	case 0:
		return span{}, false
	case 1:
		return cands[0], true
	}
	return bestCandidate(text, cands, is, claimed), true
}

// occurrences returns every place snippet occurs in text, overlapping ones
// included ("aa" occurs twice in "aaa").
func occurrences(text, snippet string) []span {
	var out []span
	for from := 0; from <= len(text)-len(snippet); {
		i := strings.Index(text[from:], snippet)
		if i < 0 {
			break
		}
		start := from + i
		from = start + 1
		end := start + len(snippet)
		// A snippet that is not valid UTF-8 could match inside a character.
		if !utf8.RuneStart(text[start]) || (end < len(text) && !utf8.RuneStart(text[end])) {
			continue
		}
		out = append(out, span{start, end})
	}
	return out
}

// bestCandidate picks among several occurrences: word-boundary matches first,
// then the best context match, then the first one not claimed by an earlier
// issue.
func bestCandidate(text string, cands []span, is Issue, claimed map[span]bool) span {
	type scored struct {
		sp       span
		boundary bool
		ctx      int
	}
	list := make([]scored, len(cands))
	for i, c := range cands {
		list[i] = scored{c, onWordBoundaries(text, c), contextScore(text, c, is)}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].boundary != list[j].boundary {
			return list[i].boundary
		}
		return list[i].ctx > list[j].ctx
	})
	// Only the best-scoring group competes on "first unclaimed"; if all of it
	// is claimed, fall back to the next group, then to the best one (which the
	// overlap rule will drop).
	for start := 0; start < len(list); {
		end := start + 1
		for end < len(list) && list[end].boundary == list[start].boundary && list[end].ctx == list[start].ctx {
			end++
		}
		group := list[start:end]
		sort.SliceStable(group, func(i, j int) bool { return group[i].sp.start < group[j].sp.start })
		for _, c := range group {
			if !claimed[c.sp] {
				return c.sp
			}
		}
		start = end
	}
	return list[0].sp
}

// onWordBoundaries reports whether the span does not start or end inside a
// word: a letter or digit at the edge of the span must not be joined to a
// letter or digit outside it.
func onWordBoundaries(text string, sp span) bool {
	first, _ := utf8.DecodeRuneInString(text[sp.start:sp.end])
	last, _ := utf8.DecodeLastRuneInString(text[sp.start:sp.end])
	if isWordRune(first) {
		if prev, _ := utf8.DecodeLastRuneInString(text[:sp.start]); sp.start > 0 && isWordRune(prev) {
			return false
		}
	}
	if isWordRune(last) {
		if next, _ := utf8.DecodeRuneInString(text[sp.end:]); sp.end < len(text) && isWordRune(next) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// contextScore is the number of runes of the issue's context_before that match
// the text just before the span (counted backwards), plus the number of runes
// of context_after that match the text just after it. Whitespace at the edge
// of the contexts is ignored because models often trim it.
func contextScore(text string, sp span, is Issue) int {
	before := strings.TrimRightFunc(text[:sp.start], unicode.IsSpace)
	after := strings.TrimLeftFunc(text[sp.end:], unicode.IsSpace)
	return commonSuffixRunes(before, strings.TrimRightFunc(is.ContextBefore, unicode.IsSpace)) +
		commonPrefixRunes(after, strings.TrimLeftFunc(is.ContextAfter, unicode.IsSpace))
}

func commonPrefixRunes(a, b string) int {
	n := 0
	for len(a) > 0 && len(b) > 0 {
		ra, wa := utf8.DecodeRuneInString(a)
		rb, wb := utf8.DecodeRuneInString(b)
		if ra != rb {
			break
		}
		n++
		a, b = a[wa:], b[wb:]
	}
	return n
}

func commonSuffixRunes(a, b string) int {
	n := 0
	for len(a) > 0 && len(b) > 0 {
		ra, wa := utf8.DecodeLastRuneInString(a)
		rb, wb := utf8.DecodeLastRuneInString(b)
		if ra != rb {
			break
		}
		n++
		a, b = a[:len(a)-wa], b[:len(b)-wb]
	}
	return n
}

// foldedText is a lower-cased copy of a text in which every run of whitespace
// is one space and typographic quotes and dashes are plain, together with, for
// each byte of the copy, the span of the original text it came from.
type foldedText struct {
	s     string
	start []int // original byte offset of the rune that produced byte i
	end   []int // original byte offset just after that rune (or whitespace run)
}

func foldText(text string) *foldedText {
	var b strings.Builder
	f := &foldedText{}
	lastSpace := true // drops leading whitespace
	for i := 0; i < len(text); {
		r, w := utf8.DecodeRuneInString(text[i:])
		start, end := i, i+w
		i = end
		if unicode.IsSpace(r) {
			if lastSpace {
				if n := len(f.end); n > 0 {
					f.end[n-1] = end // the run of spaces extends the previous space
				}
				continue
			}
			lastSpace = true
			b.WriteByte(' ')
			f.start = append(f.start, start)
			f.end = append(f.end, end)
			continue
		}
		lastSpace = false
		enc := string(foldRune(r)) // an invalid byte becomes U+FFFD, three bytes
		b.WriteString(enc)
		for range len(enc) {
			f.start = append(f.start, start)
			f.end = append(f.end, end)
		}
	}
	f.s = b.String()
	return f
}

func foldRune(r rune) rune {
	switch r {
	case '‘', '’', '‛', 'ʼ':
		return '\''
	case '“', '”', '‟':
		return '"'
	case '‐', '‑', '‒', '–', '—', '−':
		return '-'
	}
	return unicode.ToLower(r)
}

// occurrences finds snippet (folded the same way) in the folded text and maps
// the matches back to spans of the original text.
func (f *foldedText) occurrences(snippet string) []span {
	needle := foldText(snippet).s
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return nil
	}
	var out []span
	for _, m := range occurrences(f.s, needle) {
		out = append(out, span{f.start[m.start], f.end[m.end-1]})
	}
	return out
}
