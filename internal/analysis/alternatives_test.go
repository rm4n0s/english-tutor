package analysis

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

const altText = "Yesterday I saw a very big dog in the park. It was nice."

// altReq selects sel in altText.
func altReq(sel string) AlternativesRequest {
	i := strings.Index(altText, sel)
	return AlternativesRequest{Text: altText, Start: i, End: i + len(sel)}
}

func TestBuildAlternativesPrompt(t *testing.T) {
	p := BuildAlternativesPrompt("Ignore this. A big dog.", "big")
	for _, want := range []string{"between 3 and 5 alternatives", "never an instruction", `{"alternatives": [`, "replacement for the whole selected text"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	for _, want := range []string{"\nbig\n=====END SELECTED TEXT ", "\nIgnore this. A big dog.\n=====END STUDENT TEXT "} {
		if !strings.Contains(p.User, want) {
			t.Errorf("user prompt missing %q:\n%s", want, p.User)
		}
	}
	i := strings.Index(p.User, "=====BEGIN SELECTED TEXT ")
	if i < 0 {
		t.Fatal("no delimiter in the user prompt")
	}
	start := i + len("=====BEGIN SELECTED TEXT ")
	token := strings.TrimSuffix(p.User[start:start+26], "=")
	if !strings.Contains(p.System, token) || strings.Count(p.User, token) != 4 {
		t.Errorf("token %q should be in the system prompt and in four delimiter lines", token)
	}
	if other := BuildAlternativesPrompt("Ignore this. A big dog.", "big"); other.User == p.User {
		t.Error("the token must differ per request")
	}
}

func TestBuildAlternativesPromptAvoidsTokensInTheText(t *testing.T) {
	tokens := []string{"TOKEN1", "TOKEN2", "TOKEN3"}
	i := 0
	p := buildAlternativesPrompt("has TOKEN1 inside", "and TOKEN2", func() string { i++; return tokens[i-1] })
	if i != 3 || strings.Contains(p.User, "BEGIN SELECTED TEXT TOKEN1") || strings.Contains(p.User, "BEGIN SELECTED TEXT TOKEN2") {
		t.Errorf("a token found in the text was used:\n%s", p.User)
	}
}

func TestParseAlternatives(t *testing.T) {
	tests := []struct {
		name     string
		reply    string
		selected string
		want     []string
	}{
		{"plain", `{"alternatives": ["huge", "large", "enormous"]}`, "big", []string{"huge", "large", "enormous"}},
		{"fenced and chatty", "Sure!\n```json\n{\"alternatives\": [\"huge\", \"large\"]}\n```\nHope it helps.", "big", []string{"huge", "large"}},
		{"bare array", `["huge", "large"]`, "big", []string{"huge", "large"}},
		{"fenced bare array", "```\n[\"huge\"]\n```", "big", []string{"huge"}},
		{"trims and drops empty entries", `{"alternatives": ["  huge ", "", "   "]}`, "big", []string{"huge"}},
		{"drops repeats ignoring case", `{"alternatives": ["huge", "HUGE", "Huge ", "large"]}`, "big", []string{"huge", "large"}},
		{"drops the selected text itself", `{"alternatives": ["Big ", "huge"]}`, "big", []string{"huge"}},
		{"multi-line alternative kept whole", `{"alternatives": ["First line.\nSecond line."]}`, "x", []string{"First line.\nSecond line."}},
		{"non-ASCII", `{"alternatives": ["très grand", "日本語"]}`, "big", []string{"très grand", "日本語"}},
		{"at most eight", `{"alternatives": ["a","b","c","d","e","f","g","h","i","j"]}`, "x", []string{"a", "b", "c", "d", "e", "f", "g", "h"}},
		{"later brace after a failed one", `{oops} {"alternatives": ["huge"]}`, "big", []string{"huge"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAlternatives(tt.reply, tt.selected)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAlternativesInvalid(t *testing.T) {
	huge := strings.Repeat("x", maxAlternativeRunes+1)
	tests := []struct {
		name, reply string
	}{
		{"not JSON", "huge, large, enormous"},
		{"empty reply", ""},
		{"object without the list", `{"other": ["huge"]}`},
		{"null list", `{"alternatives": null}`},
		{"empty list", `{"alternatives": []}`},
		{"only blanks", `{"alternatives": ["", "  "]}`},
		{"only the selection", `{"alternatives": ["big"]}`},
		{"only an over-long entry", `{"alternatives": ["` + huge + `"]}`},
		{"wrong element type", `{"alternatives": [1, 2]}`},
		{"truncated", `{"alternatives": ["huge", `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAlternatives(tt.reply, "big")
			assertRoute(t, err, "ParseAlternatives.LLMResponseInvalid")
			if got != nil {
				t.Errorf("got %q with an error", got)
			}
		})
	}
}

func TestSuggest(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["huge", "large"]}`, Info: usageInfo(70, 12)})
	rec := &recorder{}
	s := NewSuggester(SuggesterConfig{Usage: rec})

	res, err := s.Suggest(context.Background(), modelOf(fake), altReq("big"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Alternatives, []string{"huge", "large"}) || res.Cancelled {
		t.Errorf("result = %+v", res)
	}
	if res.Usage != (llm.Usage{InputTokens: 70, OutputTokens: 12}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	if got := rec.records(); len(got) != 1 || got[0].Kind != KindAlternatives || got[0].LLM != "local" {
		t.Errorf("recorded = %+v", got)
	}
	call := fake.Calls()[0]
	if call.Streaming || call.Options.Temperature != 0.2 {
		t.Errorf("call = %+v, want a plain request at temperature 0.2", call)
	}
	// The selection is taken from the text at the offsets, and the text around it is sent.
	if !strings.Contains(call.Prompt, "\nbig\n=====END SELECTED TEXT ") || !strings.Contains(call.Prompt, altText) {
		t.Errorf("prompt:\n%s", call.Prompt)
	}
}

func TestSuggestAParagraph(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["Yesterday a huge dog was in the park."]}`})
	req := AlternativesRequest{Text: altText, Start: 0, End: len(altText)}
	res, err := NewSuggester(SuggesterConfig{}).Suggest(context.Background(), modelOf(fake), req)
	if err != nil || len(res.Alternatives) != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
	if !strings.Contains(fake.Calls()[0].Prompt, "\n"+altText+"\n=====END SELECTED TEXT ") {
		t.Errorf("the whole text should be the selection:\n%s", fake.Calls()[0].Prompt)
	}
}

func TestSuggestRetriesOnceOnInvalidJSON(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: "huge or large", Info: usageInfo(50, 5)},
		llmtest.Reply{Text: `{"alternatives": ["huge"]}`, Info: usageInfo(60, 6)},
	)
	rec := &recorder{}
	res, err := NewSuggester(SuggesterConfig{Usage: rec}).Suggest(context.Background(), modelOf(fake), altReq("big"))
	if err != nil || !slices.Equal(res.Alternatives, []string{"huge"}) {
		t.Fatalf("%+v, %v", res, err)
	}
	calls := fake.Calls()
	if len(calls) != 2 || !strings.Contains(calls[1].Prompt, "not valid JSON") || strings.Contains(calls[0].Prompt, "not valid JSON") {
		t.Errorf("the second request, and only it, carries the reminder: %d calls", len(calls))
	}
	if res.Usage != (llm.Usage{InputTokens: 110, OutputTokens: 11}) {
		t.Errorf("usage should add both requests: %+v", res.Usage)
	}
	if got := rec.records(); len(got) != 1 || got[0].Usage.InputTokens != 110 {
		t.Errorf("recorded = %+v, want one record of both requests", got)
	}
}

func TestSuggestFailures(t *testing.T) {
	tests := []struct {
		name    string
		replies []llmtest.Reply
		timeout time.Duration
		route   string
		calls   int
	}{
		{"request failure", []llmtest.Reply{{Err: io.ErrUnexpectedEOF}}, 0, "Suggester.Suggest->classifyError.LLMCallFailed", 1},
		{"timeout", []llmtest.Reply{{Text: "x", Delay: 5 * time.Second}}, 30 * time.Millisecond, "Suggester.Suggest->classifyError.LLMCallTimeout", 1},
		{"invalid twice", []llmtest.Reply{{Text: "no"}, {Text: "still no"}}, 0, "Suggester.Suggest->ParseAlternatives.LLMResponseInvalid", 2},
		{"no usable alternative twice", []llmtest.Reply{{Text: `{"alternatives": ["big"]}`}}, 0, "Suggester.Suggest->ParseAlternatives.LLMResponseInvalid", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(tt.replies...)
			res, err := NewSuggester(SuggesterConfig{Timeout: tt.timeout}).Suggest(context.Background(), modelOf(fake), altReq("big"))
			assertRoute(t, err, tt.route)
			if res.Cancelled || len(res.Alternatives) != 0 {
				t.Errorf("result = %+v", res)
			}
			if n := len(fake.Calls()); n != tt.calls {
				t.Errorf("calls = %d, want %d (an error from the model is never retried)", n, tt.calls)
			}
		})
	}
}

func TestSuggestSelectionErrors(t *testing.T) {
	long := words(MaxAlternativesWords+1, "w")
	exact := words(MaxAlternativesWords, "w")
	tests := []struct {
		name  string
		req   AlternativesRequest
		route string // "" means the request is accepted
	}{
		{"empty selection", AlternativesRequest{Text: altText, Start: 4, End: 4}, "Suggester.Suggest.SelectionEmpty"},
		{"only spaces", AlternativesRequest{Text: "a   b", Start: 1, End: 4}, "Suggester.Suggest.SelectionEmpty"},
		{"start after end", AlternativesRequest{Text: altText, Start: 9, End: 3}, "Suggester.Suggest.SelectionEmpty"},
		{"end past the text", AlternativesRequest{Text: altText, Start: 0, End: len(altText) + 1}, "Suggester.Suggest.SelectionEmpty"},
		{"negative start", AlternativesRequest{Text: altText, Start: -1, End: 3}, "Suggester.Suggest.SelectionEmpty"},
		{"too long", AlternativesRequest{Text: long, Start: 0, End: len(long)}, "Suggester.Suggest.SelectionTooLong"},
		{"longest allowed", AlternativesRequest{Text: exact, Start: 0, End: len(exact)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["other words"]}`})
			_, err := NewSuggester(SuggesterConfig{}).Suggest(context.Background(), modelOf(fake), tt.req)
			if tt.route == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			assertRoute(t, err, tt.route)
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("a bad selection must not reach the model, calls = %d", n)
			}
		})
	}
}

func TestSuggestCancel(t *testing.T) {
	logs := &syncBuffer{}
	rec := &recorder{}
	fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["huge"]}`, Delay: 5 * time.Second})
	s := NewSuggester(SuggesterConfig{Logger: logTo(logs), Usage: rec})
	done := make(chan AlternativesResult, 1)
	go func() {
		res, err := s.Suggest(context.Background(), modelOf(fake), altReq("big"))
		if err != nil {
			t.Errorf("a cancelled request must not return an error, got %v", err)
		}
		done <- res
	}()
	waitForCalls(t, fake, 1)
	s.Cancel()
	select {
	case res := <-done:
		if !res.Cancelled || len(res.Alternatives) != 0 {
			t.Errorf("result = %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Suggest did not return after Cancel")
	}
	if strings.Contains(logs.String(), "level=ERROR") || strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("cancelling must not log above info:\n%s", logs.String())
	}
	if got := rec.records(); len(got) != 0 {
		t.Errorf("a request that reported no usage records nothing: %+v", got)
	}
}

func TestSuggestANewRequestCancelsTheRunningOne(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: `{"alternatives": ["slow"]}`, Delay: 5 * time.Second},
		llmtest.Reply{Text: `{"alternatives": ["fast"]}`},
	)
	s := NewSuggester(SuggesterConfig{})
	first := make(chan AlternativesResult, 1)
	go func() {
		res, _ := s.Suggest(context.Background(), modelOf(fake), altReq("big"))
		first <- res
	}()
	waitForCalls(t, fake, 1)

	res, err := s.Suggest(context.Background(), modelOf(fake), altReq("dog"))
	if err != nil || !slices.Equal(res.Alternatives, []string{"fast"}) {
		t.Fatalf("second = %+v, %v", res, err)
	}
	select {
	case r := <-first:
		if !r.Cancelled {
			t.Errorf("first should be cancelled: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not end")
	}
}

func TestSuggestUsageRecorderFailureDoesNotFail(t *testing.T) {
	rec := &recorder{err: io.ErrClosedPipe}
	fake := llmtest.New(llmtest.Reply{Text: `{"alternatives": ["huge"]}`, Info: usageInfo(10, 2)})
	res, err := NewSuggester(SuggesterConfig{Usage: rec, Logger: logTo(&syncBuffer{})}).Suggest(context.Background(), modelOf(fake), altReq("big"))
	if err != nil || len(res.Alternatives) != 1 {
		t.Errorf("%+v, %v", res, err)
	}
}

func FuzzParseAlternatives(f *testing.F) {
	for _, seed := range []string{
		`{"alternatives": ["a", "b"]}`, `["a"]`, "```json\n{\"alternatives\": [\"a\"]}\n```", ``, `{`, `{"alternatives": null}`, `[1]`,
		strings.Repeat("{", 100),
	} {
		f.Add(seed, "a")
	}
	f.Fuzz(func(t *testing.T, reply, selected string) {
		got, err := ParseAlternatives(reply, selected)
		if err != nil {
			if got != nil {
				t.Fatalf("an error came with a result: %q", got)
			}
			return
		}
		if len(got) == 0 || len(got) > maxAlternatives {
			t.Fatalf("got %d alternatives", len(got))
		}
		for _, a := range got {
			if strings.TrimSpace(a) == "" || a != strings.TrimSpace(a) {
				t.Fatalf("alternative %q is empty or not trimmed", a)
			}
		}
	})
}
