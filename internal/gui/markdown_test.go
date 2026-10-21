package gui

import (
	"reflect"
	"testing"
)

func TestParseInline(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []mdInline
	}{
		{"plain", "He goes home", []mdInline{{Text: "He goes home"}}},
		{"bold", "Use **goes** here", []mdInline{{Text: "Use "}, {Text: "goes", Bold: true}, {Text: " here"}}},
		{"italic", "the *third* person", []mdInline{{Text: "the "}, {Text: "third", Italic: true}, {Text: " person"}}},
		{"code", "write `goes`", []mdInline{{Text: "write "}, {Text: "goes", Code: true}}},
		{"link", "see [the rule](https://example.com/r)", []mdInline{{Text: "see "}, {Text: "the rule", Link: "https://example.com/r"}}},
		{"image shows its alt text", "![a cat](http://x/y.png)", []mdInline{{Text: "a cat"}}},
		{"snake_case is text", "use snake_case names", []mdInline{{Text: "use snake_case names"}}},
		{"star around spaces is text", "2 * 3 * 4", []mdInline{{Text: "2 * 3 * 4"}}},
		{"escape", `a \* b`, []mdInline{{Text: "a * b"}}},
		{"unclosed code", "a ` b", []mdInline{{Text: "a ` b"}}},
		{"raw html stays text", "<b>x</b>", []mdInline{{Text: "<b>x</b>"}}},
		{"link title dropped", `[a](http://x "t")`, []mdInline{{Text: "a", Link: "http://x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInline(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseInline(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseMarkdown(t *testing.T) {
	md := "### What is wrong\nUse *goes*\nnot go.\n\n- one\n- two\n  more\n\n1. first\n2. second\n\n> quoted\n\n```\ncode here\n```\n\n---\n"
	got := parseMarkdown(md)
	kinds := make([]blockKind, len(got))
	for i, b := range got {
		kinds[i] = b.Kind
	}
	want := []blockKind{blockHeading, blockParagraph, blockList, blockList, blockQuote, blockCode, blockRule}
	// The heading and the paragraph above are separate blocks.
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if got[0].Level != 3 || got[0].Inlines[0].Text != "What is wrong" {
		t.Errorf("heading = %+v", got[0])
	}
	if got[2].Ordered || len(got[2].Items) != 2 || got[2].Items[1][0].Text != "two more" {
		t.Errorf("bullet list = %+v", got[2])
	}
	if !got[3].Ordered || len(got[3].Items) != 2 {
		t.Errorf("numbered list = %+v", got[3])
	}
	if got[4].Blocks[0].Inlines[0].Text != "quoted" {
		t.Errorf("quote = %+v", got[4])
	}
	if got[5].Code != "code here" {
		t.Errorf("code = %q", got[5].Code)
	}
}

func TestParseMarkdownNeverPanics(t *testing.T) {
	for _, in := range []string{"", "\n\n", "```", "> ", "- ", "[", "[a](", "**", "*", "`", "\\", "![", "# ", "1.", "> > > x"} {
		parseMarkdown(in)
	}
}

func FuzzParseMarkdown(f *testing.F) {
	for _, s := range []string{"# a\n- b\n> c\n```\nd\n```", "**a *b* `c`** [d](e)", "\\*", "![x](y)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { parseMarkdown(s) })
}
