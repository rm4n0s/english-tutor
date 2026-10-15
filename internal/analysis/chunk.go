package analysis

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DefaultChunkWords is the target maximum number of words per request.
const DefaultChunkWords = 2000

// Chunk is a piece of the text sent as one request. Text is always
// original[Start:Start+len(Text)]; the chunks of a text are contiguous and
// together cover all of it.
type Chunk struct {
	Text  string
	Start int // byte offset in the original text
}

// SplitChunks splits text into pieces of at most maxWords words (maxWords <= 0
// means DefaultChunkWords). It prefers to cut between paragraphs (blank
// lines); a paragraph that is too long is cut between sentences, and a
// sentence that is too long between words. Whitespace stays with the text
// before it, so no chunk is only whitespace. A text with no words gives no
// chunks.
func SplitChunks(text string, maxWords int) []Chunk {
	if maxWords <= 0 {
		maxWords = DefaultChunkWords
	}
	if countWords(text) == 0 {
		return nil
	}

	// Cut only at the start of an atom, where an atom is a whole paragraph if
	// it fits, else a sentence if it fits, else a run of words.
	var chunks []Chunk
	chunkStart, curWords := 0, 0
	for _, a := range atoms(text, maxWords) {
		w := countWords(text[a.start:a.end])
		if w == 0 {
			continue // blank lines: they stay with the text before them
		}
		if curWords > 0 && curWords+w > maxWords {
			chunks = append(chunks, Chunk{Text: text[chunkStart:a.start], Start: chunkStart})
			chunkStart, curWords = a.start, 0
		}
		curWords += w
	}
	return append(chunks, Chunk{Text: text[chunkStart:], Start: chunkStart})
}

// atoms returns contiguous segments covering text, each of at most maxWords
// words (blank-only segments have none).
func atoms(text string, maxWords int) []seg {
	var out []seg
	for _, para := range splitParagraphs(text) {
		if countWords(text[para.start:para.end]) <= maxWords {
			out = append(out, para)
			continue
		}
		for _, sent := range sentences(text, para) {
			if countWords(text[sent.start:sent.end]) <= maxWords {
				out = append(out, sent)
				continue
			}
			out = append(out, splitWords(text, sent, maxWords)...)
		}
	}
	return out
}

type seg struct{ start, end int }

// splitParagraphs returns contiguous segments covering text. A paragraph
// starts at the first non-blank line after a blank line; blank lines belong to
// the paragraph before them (or to the first one).
func splitParagraphs(text string) []seg {
	var out []seg
	start := 0
	pos := 0
	seenBlank := false
	for pos < len(text) {
		nl := strings.IndexByte(text[pos:], '\n')
		lineEnd := len(text)
		next := len(text)
		if nl >= 0 {
			lineEnd = pos + nl
			next = lineEnd + 1
		}
		blank := strings.TrimFunc(text[pos:lineEnd], unicode.IsSpace) == ""
		switch {
		case blank:
			seenBlank = true
		case seenBlank && pos > start:
			out = append(out, seg{start, pos})
			start = pos
			seenBlank = false
		}
		pos = next
	}
	return append(out, seg{start, len(text)})
}

// sentences splits a paragraph after sentence-ending punctuation that is
// followed by whitespace. Whitespace stays with the sentence before it.
func sentences(text string, para seg) []seg {
	var out []seg
	start := para.start
	s := text[para.start:para.end]
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		i += w
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		// Closing quotes and brackets belong to the sentence.
		j := i
		for j < len(s) {
			c, cw := utf8.DecodeRuneInString(s[j:])
			if c != '"' && c != '\'' && c != ')' && c != '”' && c != '’' {
				break
			}
			j += cw
		}
		if j >= len(s) {
			break
		}
		c, _ := utf8.DecodeRuneInString(s[j:])
		if !unicode.IsSpace(c) {
			continue
		}
		for j < len(s) {
			c, cw := utf8.DecodeRuneInString(s[j:])
			if !unicode.IsSpace(c) {
				break
			}
			j += cw
		}
		if j < len(s) {
			out = append(out, seg{start, para.start + j})
			start = para.start + j
		}
		i = j
	}
	return append(out, seg{start, para.end})
}

// splitWords cuts a segment into pieces of exactly maxWords words (the last
// may have fewer). A piece ends where the next word begins, so whitespace
// stays with the word before it.
func splitWords(text string, s seg, maxWords int) []seg {
	var out []seg
	start := s.start
	words := 0
	inWord := false
	for i := s.start; i < s.end; {
		r, w := utf8.DecodeRuneInString(text[i:s.end])
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			if words == maxWords {
				out = append(out, seg{start, i})
				start, words = i, 0
			}
			words++
		}
		i += w
	}
	return append(out, seg{start, s.end})
}

// WordCount returns the number of words in s: runs of non-space characters.
func WordCount(s string) int { return countWords(s) }

func countWords(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}
