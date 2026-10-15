package analysis

import (
	"strings"
	"testing"
	"unicode"
)

// checkChunks verifies the invariants every split must keep.
func checkChunks(t *testing.T, text string, chunks []Chunk, maxWords int) {
	t.Helper()
	pos := 0
	var joined strings.Builder
	for i, c := range chunks {
		if c.Start != pos {
			t.Fatalf("chunk %d starts at %d, want %d (chunks must be contiguous)", i, c.Start, pos)
		}
		if text[c.Start:c.Start+len(c.Text)] != c.Text {
			t.Fatalf("chunk %d text does not match text[Start:]", i)
		}
		if strings.TrimFunc(c.Text, unicode.IsSpace) == "" {
			t.Fatalf("chunk %d is only whitespace: %q", i, c.Text)
		}
		if w := countWords(c.Text); w > maxWords {
			t.Fatalf("chunk %d has %d words, max %d", i, w, maxWords)
		}
		joined.WriteString(c.Text)
		pos += len(c.Text)
	}
	if len(chunks) > 0 && joined.String() != text {
		t.Fatalf("chunks do not cover the text:\n got %q\nwant %q", joined.String(), text)
	}
}

func words(n int, prefix string) string {
	w := make([]string, n)
	for i := range w {
		w[i] = prefix
	}
	return strings.Join(w, " ")
}

func TestSplitChunks(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		maxWords  int
		wantTexts []string // nil means: only check the invariants and the count
		wantCount int
	}{
		{name: "empty", text: "", maxWords: 5, wantCount: 0},
		{name: "only whitespace", text: " \n\n\t ", maxWords: 5, wantCount: 0},
		{name: "short text is one chunk", text: "One two three.", maxWords: 5, wantTexts: []string{"One two three."}},
		{name: "default size when zero", text: "One two.", maxWords: 0, wantTexts: []string{"One two."}},
		{
			name: "paragraphs are packed up to the limit", maxWords: 6,
			text:      "a b c\n\nd e f\n\ng h i\n\nj k l",
			wantTexts: []string{"a b c\n\nd e f\n\n", "g h i\n\nj k l"},
		},
		{
			name: "two small paragraphs share a chunk", maxWords: 6,
			text:      "a b\n\nc d\n\ne f g h i j",
			wantTexts: []string{"a b\n\nc d\n\n", "e f g h i j"},
		},
		{
			name: "leading blank lines stay with the first chunk", maxWords: 3,
			text:      "\n\n  a b c\n\nd e f",
			wantTexts: []string{"\n\n  a b c\n\n", "d e f"},
		},
		{
			name: "trailing blank lines stay with the last chunk", maxWords: 3,
			text:      "a b c\n\nd e f\n\n\n",
			wantTexts: []string{"a b c\n\n", "d e f\n\n\n"},
		},
		{
			name: "blank lines with spaces separate paragraphs", maxWords: 3,
			text:      "a b c\n \t \nd e f",
			wantTexts: []string{"a b c\n \t \n", "d e f"},
		},
		{
			name: "windows line endings", maxWords: 3,
			text:      "a b c\r\n\r\nd e f",
			wantTexts: []string{"a b c\r\n\r\n", "d e f"},
		},
		{
			name: "a long paragraph is cut between sentences", maxWords: 6,
			text:      "One two three. Four five six. Seven eight nine.",
			wantTexts: []string{"One two three. Four five six. ", "Seven eight nine."},
		},
		{
			name: "sentence ends with a quote", maxWords: 5,
			text:      `He said "stop now." Then we left early today.`,
			wantTexts: []string{`He said "stop now." `, "Then we left early today."},
		},
		{
			name: "a long sentence is cut between words", maxWords: 3,
			text:      "a b c d e f g",
			wantTexts: []string{"a b c ", "d e f ", "g"},
		},
		{
			name: "abbreviation-like dots without a space do not split", maxWords: 3,
			text:      "see a.b.c now ok yes",
			wantTexts: []string{"see a.b.c now ", "ok yes"},
		},
		{
			name: "a long paragraph after a short one", maxWords: 4,
			text:      "a b\n\nc d e f. g h i j. k l",
			wantTexts: []string{"a b\n\n", "c d e f. ", "g h i j. ", "k l"},
		},
		{
			name: "unicode words", maxWords: 2,
			text:      "café 日本語 😀 naïve",
			wantTexts: []string{"café 日本語 ", "😀 naïve"},
		},
		{
			name: "exactly at the limit stays whole", maxWords: 3,
			text:      "a b c",
			wantTexts: []string{"a b c"},
		},
		{
			name: "one word over the limit is cut", maxWords: 3,
			text:      "a b c d",
			wantTexts: []string{"a b c ", "d"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := tt.maxWords
			if limit <= 0 {
				limit = DefaultChunkWords
			}
			got := SplitChunks(tt.text, tt.maxWords)
			checkChunks(t, tt.text, got, limit)
			if tt.wantTexts == nil {
				if len(got) != tt.wantCount {
					t.Fatalf("chunks = %d, want %d", len(got), tt.wantCount)
				}
				return
			}
			if len(got) != len(tt.wantTexts) {
				t.Fatalf("chunks = %q, want %q", texts(got), tt.wantTexts)
			}
			for i := range got {
				if got[i].Text != tt.wantTexts[i] {
					t.Errorf("chunk %d = %q, want %q", i, got[i].Text, tt.wantTexts[i])
				}
			}
		})
	}
}

func texts(cs []Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Text
	}
	return out
}

func TestSplitChunksLargeDocument(t *testing.T) {
	// 50 paragraphs of 120 words: about 6000 words with the default limit.
	var paras []string
	for range 50 {
		paras = append(paras, words(120, "word"))
	}
	text := strings.Join(paras, "\n\n")
	chunks := SplitChunks(text, 0)
	checkChunks(t, text, chunks, DefaultChunkWords)
	if len(chunks) != 4 { // 16 paragraphs (1920 words) per chunk -> 4 chunks
		t.Errorf("chunks = %d, want 4", len(chunks))
	}
	for i, c := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(c.Text, "\n\n") {
			t.Errorf("chunk %d should end at a paragraph boundary", i)
		}
	}
}

func TestSplitChunksOneHugeParagraph(t *testing.T) {
	// A single 5000-word paragraph without sentence ends.
	text := words(5000, "x")
	chunks := SplitChunks(text, 0)
	checkChunks(t, text, chunks, DefaultChunkWords)
	if len(chunks) != 3 {
		t.Errorf("chunks = %d, want 3", len(chunks))
	}
}

func FuzzSplitChunks(f *testing.F) {
	for _, s := range []string{"", "a", "a b c. d e f.\n\ng h", "\n\n x \n\n", "😀 é\r\n\r\nz", "a.  b!  c?\"  d"} {
		f.Add(s, 2)
	}
	f.Fuzz(func(t *testing.T, text string, maxWords int) {
		if maxWords < 1 || maxWords > 50 {
			maxWords = 3
		}
		chunks := SplitChunks(text, maxWords)
		if countWords(text) == 0 {
			if len(chunks) != 0 {
				t.Fatalf("expected no chunks for %q", text)
			}
			return
		}
		checkChunks(t, text, chunks, maxWords)
	})
}
