// Package analysis holds the prompts, the response contract, the error type
// taxonomy and (in later files) issue locating and chunking. It does not import
// a GUI toolkit, so it can be tested without a display.
package analysis

import "strings"

// Category groups error types. The editor colours underlines by category.
type Category string

// The three categories, in the order they are listed in the legend.
const (
	Grammar    Category = "grammar"
	Syntax     Category = "syntax"
	Vocabulary Category = "vocabulary"
)

// Label returns the category's display name.
func (c Category) Label() string {
	switch c {
	case Grammar:
		return "Grammar"
	case Syntax:
		return "Syntax"
	case Vocabulary:
		return "Vocabulary"
	default:
		return string(c)
	}
}

// Categories lists every category in display order.
func Categories() []Category { return []Category{Grammar, Syntax, Vocabulary} }

// ErrorType is one entry of the fixed taxonomy. IDs are stable: statistics
// store them as text, so a label may change but an ID must not.
type ErrorType struct {
	ID       string
	Label    string
	Category Category
}

// otherID returns the catch-all type id of a category.
func otherID(c Category) string { return string(c) + "_other" }

var taxonomy = []ErrorType{
	{"subject_verb_agreement", "Subject-verb agreement", Grammar},
	{"verb_tense", "Verb tense", Grammar},
	{"verb_form", "Verb form", Grammar},
	{"modal_verb", "Modal verb", Grammar},
	{"article", "Article", Grammar},
	{"preposition", "Preposition", Grammar},
	{"plural_singular", "Singular/plural", Grammar},
	{"countable_uncountable", "Countable/uncountable", Grammar},
	{"pronoun", "Pronoun", Grammar},
	{"comparative_superlative", "Comparative/superlative", Grammar},
	{"grammar_other", "Other grammar", Grammar},

	{"word_order", "Word order", Syntax},
	{"missing_word", "Missing word", Syntax},
	{"extra_word", "Extra word", Syntax},
	{"sentence_fragment", "Sentence fragment", Syntax},
	{"run_on_sentence", "Run-on sentence", Syntax},
	{"conditional_structure", "Conditional structure", Syntax},
	{"syntax_other", "Other syntax (including punctuation and capitalisation)", Syntax},

	{"wrong_word", "Wrong word", Vocabulary},
	{"confused_words", "Confused words", Vocabulary},
	{"collocation", "Collocation", Vocabulary},
	{"spelling", "Spelling", Vocabulary},
	{"vocabulary_other", "Other vocabulary", Vocabulary},
}

// Types returns a copy of the whole taxonomy in display order.
func Types() []ErrorType {
	return append([]ErrorType(nil), taxonomy...)
}

// Lookup returns the type with exactly this id.
func Lookup(id string) (ErrorType, bool) {
	for _, t := range taxonomy {
		if t.ID == id {
			return t, true
		}
	}
	return ErrorType{}, false
}

// Resolve maps whatever id the model returned to a taxonomy entry. A known id
// (ignoring case, surrounding space, and spaces or hyphens used for
// underscores) gives that type. Anything else becomes the "_other" type of the
// category its wording best suggests, or grammar_other if it suggests none. An
// unknown id is not an error.
func Resolve(id string) ErrorType {
	norm := normaliseID(id)
	if t, ok := Lookup(norm); ok {
		return t
	}
	t, _ := Lookup(otherID(guessCategory(norm)))
	return t
}

func normaliseID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.NewReplacer(" ", "_", "-", "_").Replace(id)
}

// categoryHints are words that suggest a category, checked as substrings of a
// normalised id. The category with the most hits wins; a tie goes to the
// earlier category in Categories().
var categoryHints = map[Category][]string{
	Grammar:    {"grammar", "tense", "verb", "agreement", "article", "preposition", "plural", "singular", "pronoun", "modal", "countable", "comparative", "superlative", "inflect"},
	Syntax:     {"syntax", "order", "sentence", "clause", "fragment", "punctuation", "capital", "structure", "conditional", "missing", "extra", "comma", "run_on"},
	Vocabulary: {"vocab", "word", "spell", "collocation", "lexical", "lexis", "diction", "usage", "idiom", "confus", "typo"},
}

func guessCategory(norm string) Category {
	best, bestHits := Grammar, 0
	for _, c := range Categories() {
		hits := 0
		for _, h := range categoryHints[c] {
			if strings.Contains(norm, h) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = c, hits
		}
	}
	return best
}
