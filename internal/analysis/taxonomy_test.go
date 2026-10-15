package analysis

import (
	"strings"
	"testing"
)

func TestTaxonomyMatchesSpec(t *testing.T) {
	want := map[Category][]string{
		Grammar: {
			"subject_verb_agreement", "verb_tense", "verb_form", "modal_verb", "article", "preposition",
			"plural_singular", "countable_uncountable", "pronoun", "comparative_superlative", "grammar_other",
		},
		Syntax: {
			"word_order", "missing_word", "extra_word", "sentence_fragment", "run_on_sentence",
			"conditional_structure", "syntax_other",
		},
		Vocabulary: {"wrong_word", "confused_words", "collocation", "spelling", "vocabulary_other"},
	}
	got := map[Category][]string{}
	seen := map[string]bool{}
	for _, typ := range Types() {
		if typ.Label == "" {
			t.Errorf("%s has no label", typ.ID)
		}
		if seen[typ.ID] {
			t.Errorf("duplicate id %s", typ.ID)
		}
		seen[typ.ID] = true
		got[typ.Category] = append(got[typ.Category], typ.ID)
	}
	for _, c := range Categories() {
		if strings.Join(got[c], ",") != strings.Join(want[c], ",") {
			t.Errorf("%s types = %v, want %v", c, got[c], want[c])
		}
		if _, ok := Lookup(otherID(c)); !ok {
			t.Errorf("category %s has no %s type", c, otherID(c))
		}
	}
	if len(got) != len(want) {
		t.Errorf("categories in taxonomy = %d, want %d", len(got), len(want))
	}
}

func TestTypesReturnsACopy(t *testing.T) {
	a := Types()
	a[0].ID = "changed"
	if Types()[0].ID == "changed" {
		t.Error("Types() exposes the internal slice")
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		id     string
		wantOK bool
		want   Category
	}{
		{"verb_tense", true, Grammar},
		{"spelling", true, Vocabulary},
		{"word_order", true, Syntax},
		{"Verb_Tense", false, ""},
		{"", false, ""},
	}
	for _, tt := range tests {
		got, ok := Lookup(tt.id)
		if ok != tt.wantOK || (ok && got.Category != tt.want) {
			t.Errorf("Lookup(%q) = %+v, %v", tt.id, got, ok)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"known id", "verb_tense", "verb_tense"},
		{"known id with different case and space", "  Verb Tense ", "verb_tense"},
		{"known id with hyphens", "subject-verb-agreement", "subject_verb_agreement"},
		{"category name only", "grammar", "grammar_other"},
		{"vocabulary wording", "vocabulary error", "vocabulary_other"},
		{"word choice wording", "word_choice_issue", "vocabulary_other"},
		{"punctuation", "punctuation", "syntax_other"},
		{"capitalisation", "capitalization", "syntax_other"},
		{"unknown grammar wording", "tense_consistency", "grammar_other"},
		{"unknown syntax wording", "clause_structure_problem", "syntax_other"},
		{"nothing suggests a category", "zzz", "grammar_other"},
		{"empty", "", "grammar_other"},
		{"very long", strings.Repeat("x", 10000), "grammar_other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.in); got.ID != tt.want {
				t.Errorf("Resolve(%q) = %s, want %s", tt.in, got.ID, tt.want)
			}
		})
	}
}

func TestCategoryLabel(t *testing.T) {
	for _, c := range Categories() {
		if c.Label() == "" || c.Label() == string(c) {
			t.Errorf("category %s has label %q", c, c.Label())
		}
	}
	if got := Category("other").Label(); got != "other" {
		t.Errorf("unknown category label = %q", got)
	}
}
