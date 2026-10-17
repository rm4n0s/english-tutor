package analysis

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

const oneIssue = `{"issues":[{"text":"He go","error_type":"subject_verb_agreement","message":"Use goes.","context_before":"Yesterday ","context_after":" home"}]}`

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name        string
		reply       string
		wantTexts   []string
		wantTypes   []string
		wantDropped int
	}{
		{"plain object", oneIssue, []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"no issues", `{"issues": []}`, nil, nil, 0},
		{"json fence", "```json\n" + oneIssue + "\n```", []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"plain fence", "```\n" + oneIssue + "\n```\n", []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"prose before and after", "Sure, here you go:\n" + oneIssue + "\nHope this helps!", []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"braces in the prose before", "Result {see below}: " + oneIssue, []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"thinking before the answer", "<think>Maybe {a}, {b}, {c}, {d}, {e}, {f}, {g}, {h}, {i}. Draft: " + `{"issues":[{"text":"draft","error_type":"article"}]}` + "</think>\n" + oneIssue, []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"leading byte order mark", "\xef\xbb\xbf" + oneIssue, []string{"He go"}, []string{"subject_verb_agreement"}, 0},
		{"bare array", `[{"text":"a b","error_type":"article"}]`, []string{"a b"}, []string{"article"}, 0},
		{"unknown fields are ignored", `{"issues":[{"text":"x","error_type":"article","severity":"high"}],"summary":"ok"}`, []string{"x"}, []string{"article"}, 0},
		{"unknown type becomes other", `{"issues":[{"text":"x","error_type":"punctuation"}]}`, []string{"x"}, []string{"syntax_other"}, 0},
		{"missing type becomes grammar_other", `{"issues":[{"text":"x"}]}`, []string{"x"}, []string{"grammar_other"}, 0},
		{"empty text is dropped", `{"issues":[{"text":"","error_type":"article"},{"text":"  ","error_type":"article"},{"text":"ok","error_type":"article"}]}`, []string{"ok"}, []string{"article"}, 2},
		{"snippet is trimmed", `{"issues":[{"text":"  go  ","error_type":"verb_form"}]}`, []string{"go"}, []string{"verb_form"}, 0},
		{"several issues keep order", `{"issues":[{"text":"a","error_type":"article"},{"text":"b","error_type":"spelling"}]}`, []string{"a", "b"}, []string{"article", "spelling"}, 0},
		{"unicode text", `{"issues":[{"text":"café 😀","error_type":"spelling"}]}`, []string{"café 😀"}, []string{"spelling"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.reply)
			if err != nil {
				t.Fatalf("ParseResponse() error = %v", err)
			}
			if got.Dropped != tt.wantDropped {
				t.Errorf("Dropped = %d, want %d", got.Dropped, tt.wantDropped)
			}
			if len(got.Issues) != len(tt.wantTexts) {
				t.Fatalf("issues = %+v, want texts %v", got.Issues, tt.wantTexts)
			}
			for i, is := range got.Issues {
				if is.Text != tt.wantTexts[i] || is.Type.ID != tt.wantTypes[i] {
					t.Errorf("issue %d = %q/%s, want %q/%s", i, is.Text, is.Type.ID, tt.wantTexts[i], tt.wantTypes[i])
				}
			}
		})
	}
}

func TestParseResponseFields(t *testing.T) {
	got, err := ParseResponse(oneIssue)
	if err != nil {
		t.Fatal(err)
	}
	is := got.Issues[0]
	if is.Message != "Use goes." || is.ContextBefore != "Yesterday " || is.ContextAfter != " home" {
		t.Errorf("issue = %+v", is)
	}
	if is.Type.Category != Grammar {
		t.Errorf("category = %s", is.Type.Category)
	}
}

func TestParseResponseDefaultsAndLimits(t *testing.T) {
	long := strings.Repeat("é", 1000)
	reply := `{"issues":[{"text":"x","error_type":"spelling","message":"","context_before":"` + long + `","context_after":"` + long + `"},` +
		`{"text":"y","error_type":"spelling","message":"` + long + `"}]}`
	got, err := ParseResponse(reply)
	if err != nil {
		t.Fatal(err)
	}
	if got.Issues[0].Message != "Spelling" {
		t.Errorf("empty message should fall back to the type label, got %q", got.Issues[0].Message)
	}
	if n := len([]rune(got.Issues[0].ContextBefore)); n != maxContextRunes {
		t.Errorf("context_before runes = %d, want %d", n, maxContextRunes)
	}
	if n := len([]rune(got.Issues[0].ContextAfter)); n != maxContextRunes {
		t.Errorf("context_after runes = %d, want %d", n, maxContextRunes)
	}
	if n := len([]rune(got.Issues[1].Message)); n != maxMessageRunes {
		t.Errorf("message runes = %d, want %d", n, maxMessageRunes)
	}
}

func TestParseResponseDropsHugeSnippet(t *testing.T) {
	reply := `{"issues":[{"text":"` + strings.Repeat("a", maxSnippetRunes+1) + `","error_type":"article"}]}`
	got, err := ParseResponse(reply)
	if err != nil || len(got.Issues) != 0 || got.Dropped != 1 {
		t.Errorf("ParseResponse() = %+v, %v; want the snippet dropped", got, err)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	tests := []struct{ name, reply string }{
		{"empty", ""},
		{"whitespace", "  \n "},
		{"prose", "I found no errors in your text. Well done!"},
		{"truncated json", `{"issues":[{"text":"a","error_type":"article"`},
		{"object without issues", `{"errors":[]}`},
		{"issues is null", `{"issues": null}`},
		{"issues is a string", `{"issues": "none"}`},
		{"issues entries of the wrong type", `{"issues": [1, 2]}`},
		{"empty object", `{}`},
		{"null", `null`},
		{"number", `42`},
		{"only a fence", "```json\n```"},
		{"many braces and no json", strings.Repeat("{ ", 10000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResponse(tt.reply)
			if err == nil {
				t.Fatal("expected an error")
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("ParseResponse.LLMResponseInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if appErr.Message == "" {
				t.Error("the message is shown to the user and must not be empty")
			}
			for k, v := range appErr.Metadata {
				if s, ok := v.(string); ok && strings.Contains(s, "found no errors") {
					t.Errorf("metadata %q contains the model's reply", k)
				}
			}
		})
	}
}

func FuzzParseResponse(f *testing.F) {
	for _, seed := range []string{oneIssue, `{"issues":[]}`, "```json\n{}\n```", `[{"text":"a"}]`, "", "{", "[{", `{"issues":[{"text":"\ud800"}]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, reply string) {
		got, err := ParseResponse(reply)
		if err != nil {
			return
		}
		for _, is := range got.Issues {
			if is.Text == "" || is.Type.ID == "" {
				t.Fatalf("invalid issue accepted: %+v", is)
			}
			if _, ok := Lookup(is.Type.ID); !ok {
				t.Fatalf("type %q is not in the taxonomy", is.Type.ID)
			}
		}
	})
}
