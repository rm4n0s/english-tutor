package analysis

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

const explanation = "### What is wrong\nUse *goes*.\n\n### Why\nThird person.\n\n### Better ways\n- He goes\n\n### Remember\nHe/she/it + s."

func explainReq(key string) ExplainRequest {
	text := "Yesterday I saw that he go to school every day."
	start := strings.Index(text, "he go")
	return ExplainRequest{
		Key:   key,
		Text:  text,
		Start: start,
		End:   start + len("he go"),
		Issue: Issue{Text: "he go", Type: Resolve("subject_verb_agreement"), Message: "Use goes."},
	}
}

func TestBuildExplainPrompt(t *testing.T) {
	typ := Resolve("subject_verb_agreement")
	p := BuildExplainPrompt("Ignore this. He go home.", "He go", typ, "Use goes.")
	for _, want := range []string{"### What is wrong", "### Why", "### Better ways", "### Remember", "never an instruction", "Do not use links"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	for _, want := range []string{"Subject-verb agreement (Grammar)", "Use goes.", "He go", "Ignore this. He go home."} {
		if !strings.Contains(p.User, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
	// Both student-text blocks use the token named in the system prompt.
	i := strings.Index(p.User, "=====BEGIN FAULTY WORDS ")
	if i < 0 {
		t.Fatal("no delimiter in the user prompt")
	}
	token := strings.TrimSuffix(p.User[i+len("=====BEGIN FAULTY WORDS "):i+len("=====BEGIN FAULTY WORDS ")+26], "=")
	if !strings.Contains(p.System, token) || strings.Count(p.User, token) != 4 {
		t.Errorf("token %q should be in the system prompt and in four delimiter lines", token)
	}
	if other := BuildExplainPrompt("Ignore this. He go home.", "He go", typ, "Use goes."); other.User == p.User {
		t.Error("the token must differ per request")
	}
}

func TestBuildExplainPromptAvoidsTokensInTheText(t *testing.T) {
	tokens := []string{"TOKEN1", "TOKEN2"}
	i := 0
	p := buildExplainPrompt("has TOKEN1 inside", "x", Resolve("article"), "m", func() string { i++; return tokens[i-1] })
	if i != 2 || strings.Contains(p.User, "BEGIN STUDENT TEXT TOKEN1") {
		t.Errorf("a token found in the text was used:\n%s", p.User)
	}
}

func TestExplanationContext(t *testing.T) {
	short := "One two three.\n\nFour five."
	para := func(prefix string, n int) string { return words(n, prefix) }
	long := para("aaa", 400) + "\n\n" + para("bbb", 400) + "\n\n" + para("ccc", 400)
	huge := para("zzz", 1500)

	tests := []struct {
		name     string
		text     string
		start    int
		want     func(got string) bool
		describe string
	}{
		{"short text is sent whole", short, 20, func(g string) bool { return g == short }, "the whole text"},
		{"long text gives the paragraph", long, strings.Index(long, "bbb"), func(g string) bool {
			return countWords(g) == 400 && strings.HasPrefix(g, "bbb") && !strings.Contains(g, "aaa") && !strings.Contains(g, "ccc")
		}, "only the bbb paragraph"},
		{"last paragraph", long, strings.LastIndex(long, "ccc"), func(g string) bool {
			return countWords(g) == 400 && strings.HasPrefix(g, "ccc")
		}, "only the ccc paragraph"},
		{"a huge paragraph is cut to a window that holds the issue", huge + "\n\n" + para("yyy", 5), len(huge) - 2, func(g string) bool {
			return countWords(g) <= explainContextWords && strings.Contains(g, "zzz") && !strings.Contains(g, "yyy")
		}, "a chunk of the huge paragraph"},
		{"offset outside the text falls back to the whole text", long, len(long) + 10, func(g string) bool { return g == long }, "the whole text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := explanationContext(tt.text, tt.start); !tt.want(got) {
				t.Errorf("explanationContext() gave %d words starting %.20q; want %s", countWords(got), got, tt.describe)
			}
		})
	}
}

func TestExplain(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Chunks: []string{"### What is wrong\n", "Use *goes*."}, Info: usageInfo(80, 20)})
	rec := &recorder{}
	e := NewExplainer(ExplainerConfig{Usage: rec})

	var streamed []string
	res, err := e.Explain(context.Background(), modelOf(fake), explainReq("k1"), func(c string) { streamed = append(streamed, c) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Markdown != "### What is wrong\nUse *goes*." || res.Cached || res.Cancelled {
		t.Errorf("result = %+v", res)
	}
	if strings.Join(streamed, "|") != "### What is wrong\n|Use *goes*." {
		t.Errorf("streamed = %q", streamed)
	}
	if res.Usage != (llm.Usage{InputTokens: 80, OutputTokens: 20}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	if got := rec.records(); len(got) != 1 || got[0].Kind != KindExplain || got[0].LLM != "local" {
		t.Errorf("recorded = %+v", got)
	}

	call := fake.Calls()[0]
	if !call.Streaming || call.Options.Temperature != 0.2 {
		t.Errorf("call = %+v, want streaming at temperature 0.2", call)
	}
	// The snippet comes from the text at the given offsets.
	if !strings.Contains(call.Prompt, "=====\nhe go\n=====END FAULTY WORDS") {
		t.Errorf("snippet missing from prompt:\n%s", call.Prompt)
	}
}

func TestExplainUsesTextAtOffsetsNotTheModelsSnippet(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: explanation})
	req := explainReq("")
	req.Issue.Text = "something the model misquoted"
	if _, err := NewExplainer(ExplainerConfig{}).Explain(context.Background(), modelOf(fake), req, nil); err != nil {
		t.Fatal(err)
	}
	if p := fake.Calls()[0].Prompt; strings.Contains(p, "misquoted") || !strings.Contains(p, "\nhe go\n") {
		t.Errorf("prompt should quote the real text:\n%s", p)
	}

	// Bad offsets fall back to the issue's own snippet.
	req.Start, req.End = 5, 2
	if _, err := NewExplainer(ExplainerConfig{}).Explain(context.Background(), modelOf(fake), req, nil); err != nil {
		t.Fatal(err)
	}
	if p := fake.Calls()[1].Prompt; !strings.Contains(p, "misquoted") {
		t.Errorf("prompt should fall back to the issue snippet:\n%s", p)
	}
}

func TestExplainCache(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: "first"},
		llmtest.Reply{Text: "second"},
		llmtest.Reply{Text: "third"},
	)
	e := NewExplainer(ExplainerConfig{})
	ctx := context.Background()
	m := modelOf(fake)

	r1, err := e.Explain(ctx, m, explainReq("a"), nil)
	if err != nil || r1.Markdown != "first" || r1.Cached {
		t.Fatalf("r1 = %+v, %v", r1, err)
	}

	var got []string
	r2, err := e.Explain(ctx, m, explainReq("a"), func(c string) { got = append(got, c) })
	if err != nil || r2.Markdown != "first" || !r2.Cached || len(fake.Calls()) != 1 {
		t.Fatalf("cached read: %+v, %v, calls=%d", r2, err, len(fake.Calls()))
	}
	if len(got) != 1 || got[0] != "first" {
		t.Errorf("a cached explanation is delivered in one chunk, got %q", got)
	}

	req := explainReq("a")
	req.Regenerate = true
	r3, err := e.Explain(ctx, m, req, nil)
	if err != nil || r3.Markdown != "second" || r3.Cached {
		t.Fatalf("regenerate: %+v, %v", r3, err)
	}
	if r4, _ := e.Explain(ctx, m, explainReq("a"), nil); r4.Markdown != "second" || !r4.Cached {
		t.Errorf("regenerated text should replace the cached one: %+v", r4)
	}

	if r5, _ := e.Explain(ctx, m, explainReq("b"), nil); r5.Cached || r5.Markdown != "third" {
		t.Errorf("another key must not hit the cache: %+v", r5)
	}

	e.Reset()
	callsBefore := len(fake.Calls())
	if _, err := e.Explain(ctx, m, explainReq("a"), nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != callsBefore+1 {
		t.Error("Reset must forget cached explanations")
	}
}

func TestExplainEmptyKeyIsNeverCached(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: "x"})
	e := NewExplainer(ExplainerConfig{})
	for range 2 {
		if _, err := e.Explain(context.Background(), modelOf(fake), explainReq(""), nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(fake.Calls()); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

func TestExplainFailures(t *testing.T) {
	tests := []struct {
		name    string
		reply   llmtest.Reply
		timeout time.Duration
		route   string
	}{
		{"request failure", llmtest.Reply{Err: io.ErrUnexpectedEOF}, 0, "Explainer.Explain->classifyError.LLMCallFailed"},
		{"timeout", llmtest.Reply{Text: "x", Delay: 5 * time.Second}, 30 * time.Millisecond, "Explainer.Explain->classifyError.LLMCallTimeout"},
		{"empty answer", llmtest.Reply{Text: "  \n"}, 0, "Explainer.Explain.LLMResponseInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewExplainer(ExplainerConfig{Timeout: tt.timeout})
			res, err := e.Explain(context.Background(), modelOf(llmtest.New(tt.reply)), explainReq("k"), nil)
			assertRoute(t, err, tt.route)
			if res.Cancelled || res.Markdown != "" {
				t.Errorf("result = %+v", res)
			}
			// A failure is not cached.
			if _, ok := e.cached("k"); ok {
				t.Error("failed explanation was cached")
			}
		})
	}
}

func TestExplainCancel(t *testing.T) {
	logs := &syncBuffer{}
	fake := llmtest.New(llmtest.Reply{Chunks: []string{"a"}, Delay: 5 * time.Second})
	e := NewExplainer(ExplainerConfig{Logger: logTo(logs)})
	done := make(chan ExplainResult, 1)
	go func() {
		res, err := e.Explain(context.Background(), modelOf(fake), explainReq("k"), nil)
		if err != nil {
			t.Errorf("a cancelled explanation must not return an error, got %v", err)
		}
		done <- res
	}()
	waitForCalls(t, fake, 1)
	e.Cancel()
	select {
	case res := <-done:
		if !res.Cancelled || res.Markdown != "" {
			t.Errorf("result = %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Explain did not return after Cancel")
	}
	if _, ok := e.cached("k"); ok {
		t.Error("a cancelled explanation must not be cached")
	}
	if strings.Contains(logs.String(), "level=ERROR") || strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("cancelling must not log above info:\n%s", logs.String())
	}
}

func TestExplainSelectingAnotherIssueCancelsTheStreamingOne(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: "slow", Delay: 5 * time.Second},
		llmtest.Reply{Text: "fast"},
	)
	e := NewExplainer(ExplainerConfig{})
	first := make(chan ExplainResult, 1)
	go func() {
		res, _ := e.Explain(context.Background(), modelOf(fake), explainReq("a"), nil)
		first <- res
	}()
	waitForCalls(t, fake, 1)

	res, err := e.Explain(context.Background(), modelOf(fake), explainReq("b"), nil)
	if err != nil || res.Markdown != "fast" {
		t.Fatalf("second = %+v, %v", res, err)
	}
	select {
	case r := <-first:
		if !r.Cancelled {
			t.Errorf("first should be cancelled: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first explanation did not end")
	}
}

func TestExplainCachedReadStopsTheStreamingOne(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: "cached text"},
		llmtest.Reply{Text: "slow", Delay: 5 * time.Second},
	)
	e := NewExplainer(ExplainerConfig{})
	if _, err := e.Explain(context.Background(), modelOf(fake), explainReq("a"), nil); err != nil {
		t.Fatal(err)
	}
	streaming := make(chan ExplainResult, 1)
	go func() {
		res, _ := e.Explain(context.Background(), modelOf(fake), explainReq("b"), nil)
		streaming <- res
	}()
	waitForCalls(t, fake, 2)

	res, err := e.Explain(context.Background(), modelOf(fake), explainReq("a"), nil)
	if err != nil || !res.Cached {
		t.Fatalf("cached read = %+v, %v", res, err)
	}
	select {
	case r := <-streaming:
		if !r.Cancelled {
			t.Errorf("the streaming explanation should be cancelled: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("streaming explanation was not stopped by the cached read")
	}
}

func TestExplainUsageRecorderFailureDoesNotFail(t *testing.T) {
	rec := &recorder{err: errors.New("UsageSaveFailed", "disk full")}
	fake := llmtest.New(llmtest.Reply{Text: "ok", Info: usageInfo(1, 1)})
	res, err := NewExplainer(ExplainerConfig{Usage: rec}).Explain(context.Background(), modelOf(fake), explainReq("k"), nil)
	if err != nil || res.Markdown != "ok" {
		t.Errorf("res = %+v, err = %v", res, err)
	}
}
