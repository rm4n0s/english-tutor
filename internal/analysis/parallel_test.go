package analysis

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

// probeModel answers each request according to its prompt, and records how many
// requests ran at the same time.
type probeModel struct {
	mu         sync.Mutex
	running    int
	maxRunning int
	delay      time.Duration
	// reply returns the answer for a prompt; an error fails the request.
	reply func(prompt string) (string, error)
}

func (p *probeModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	var prompt strings.Builder
	for _, m := range messages {
		for _, part := range m.Parts {
			if t, ok := part.(llms.TextContent); ok {
				prompt.WriteString(t.Text)
			}
		}
	}
	p.mu.Lock()
	p.running++
	p.maxRunning = max(p.maxRunning, p.running)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.running--
		p.mu.Unlock()
	}()

	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	text, err := p.reply(prompt.String())
	if err != nil {
		return nil, err
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: text}}}, nil
}

func (p *probeModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, p, prompt, options...)
}

func (p *probeModel) peak() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxRunning
}

// issueFor answers a prompt with the issue of the paragraph it contains.
func issueFor(prompt string) (string, error) {
	for _, bad := range []string{"Quokka go", "Wombat have", "Dingo is", "Numbat was"} {
		if strings.Contains(prompt, bad) {
			return issuesJSON(bad), nil
		}
	}
	return issuesJSON(), nil
}

const fourParagraphs = "Quokka go home today.\n\nWombat have a cat.\n\nDingo is happy now.\n\nNumbat was late again."

func TestCheckParallel(t *testing.T) {
	tests := []struct {
		name         string
		maxParallel  int
		wantPeak     int
		wantAtLeast2 bool
	}{
		{"default is one at a time", 0, 1, false},
		{"one", 1, 1, false},
		{"two", 2, 2, true},
		{"limit is respected", 3, 3, true},
		{"more than chunks", 8, 4, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &probeModel{delay: 40 * time.Millisecond, reply: issueFor}
			m := Model{Name: "local", LLM: probe, MaxParallel: tt.maxParallel}
			res, err := NewAnalyzer(AnalyzerConfig{ChunkWords: 4}).Check(context.Background(), m, fourParagraphs)
			if err != nil {
				t.Fatal(err)
			}
			if res.Chunks != 4 || len(res.Placed) != 4 {
				t.Fatalf("chunks = %d, placed = %d; want 4 and 4", res.Chunks, len(res.Placed))
			}
			// Whatever order the requests finished in, the issues are in text order.
			for i, want := range []string{"Quokka go", "Wombat have", "Dingo is", "Numbat was"} {
				p := res.Placed[i]
				if got := fourParagraphs[p.Start:p.End]; got != want {
					t.Errorf("issue %d = %q, want %q", i, got, want)
				}
			}
			if got := probe.peak(); got != tt.wantPeak {
				t.Errorf("most requests at once = %d, want %d", got, tt.wantPeak)
			}
		})
	}
}

func TestCheckParallelFailureStopsTheOthers(t *testing.T) {
	probe := &probeModel{delay: 5 * time.Second, reply: issueFor}
	failing := &probeModel{reply: func(string) (string, error) { return "", io.ErrUnexpectedEOF }}
	// The second paragraph fails at once; the others would take 5 s.
	model := llmFunc(func(ctx context.Context, msgs []llms.MessageContent, o ...llms.CallOption) (*llms.ContentResponse, error) {
		if strings.Contains(promptOf(msgs), "Wombat have") {
			return failing.GenerateContent(ctx, msgs, o...)
		}
		return probe.GenerateContent(ctx, msgs, o...)
	})
	rec := &recorder{}
	started := time.Now()
	res, err := NewAnalyzer(AnalyzerConfig{ChunkWords: 4, Usage: rec}).Check(context.Background(),
		Model{Name: "local", LLM: model, MaxParallel: 4}, fourParagraphs)
	assertRoute(t, err, "classifyError.LLMCallFailed")
	if res.Cancelled || len(res.Placed) != 0 {
		t.Errorf("result = %+v; a failed check has no issues and is not a cancellation", res)
	}
	if time.Since(started) > 2*time.Second {
		t.Errorf("the check waited %v for requests that should have been cancelled", time.Since(started))
	}
}

func TestCheckParallelFirstFailureIsReported(t *testing.T) {
	// A timeout on one chunk is the error, not the cancellation of the others.
	model := llmFunc(func(ctx context.Context, msgs []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	_, err := NewAnalyzer(AnalyzerConfig{ChunkWords: 4}).Check(context.Background(),
		Model{Name: "local", LLM: model, MaxParallel: 4, Timeout: 30 * time.Millisecond}, fourParagraphs)
	assertRoute(t, err, "classifyError.LLMCallTimeout")
}

func TestCheckParallelCancel(t *testing.T) {
	probe := &probeModel{delay: 5 * time.Second, reply: issueFor}
	a := NewAnalyzer(AnalyzerConfig{ChunkWords: 4})
	done := make(chan CheckResult, 1)
	go func() {
		res, err := a.Check(context.Background(), Model{Name: "local", LLM: probe, MaxParallel: 4}, fourParagraphs)
		if err != nil {
			t.Errorf("a cancelled check must not return an error, got %v", err)
		}
		done <- res
	}()
	deadline := time.Now().Add(5 * time.Second)
	for probe.peak() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	a.Cancel()
	select {
	case res := <-done:
		if !res.Cancelled || len(res.Placed) != 0 {
			t.Errorf("result = %+v, want cancelled with no issues", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cancel did not end the parallel requests")
	}
}

func TestCheckParallelSumsUsage(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON(), Info: usageInfo(10, 2)})
	rec := &recorder{}
	res, err := NewAnalyzer(AnalyzerConfig{ChunkWords: 4, Usage: rec}).Check(context.Background(),
		Model{Name: "local", LLM: fake, MaxParallel: 4}, fourParagraphs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != 40 || res.Usage.OutputTokens != 8 || len(rec.records()) != 4 {
		t.Errorf("usage = %+v with %d records, want 40/8 and 4 records", res.Usage, len(rec.records()))
	}
}

func TestModelTimeoutOverridesTheDefault(t *testing.T) {
	slow := llmtest.Reply{Text: issuesJSON(), Delay: 5 * time.Second}
	m := Model{Name: "local", LLM: llmtest.New(slow), Timeout: 30 * time.Millisecond}

	// The analyzer's own default is long; the model's timeout wins.
	_, err := NewAnalyzer(AnalyzerConfig{Timeout: time.Hour}).Check(context.Background(), m, "He go home.")
	assertRoute(t, err, "classifyError.LLMCallTimeout")
	if appErr, _ := errors.FromError(err); !strings.Contains(appErr.Message, "30ms") {
		t.Errorf("message %q should name the timeout of the model", appErr.Message)
	}

	m.LLM = llmtest.New(llmtest.Reply{Text: "x", Delay: 5 * time.Second})
	_, err = NewExplainer(ExplainerConfig{Timeout: time.Hour}).Explain(context.Background(), m, explainReq("k"), nil)
	assertRoute(t, err, "Explainer.Explain->classifyError.LLMCallTimeout")

	m.LLM = llmtest.New(llmtest.Reply{Text: "x", Delay: 5 * time.Second})
	_, err = NewSuggester(SuggesterConfig{Timeout: time.Hour}).Suggest(context.Background(), m, altReq("big"))
	assertRoute(t, err, "Suggester.Suggest->classifyError.LLMCallTimeout")
}

func TestChunkSize(t *testing.T) {
	words := func(n int) string { return strings.TrimSpace(strings.Repeat("word ", n)) }
	tests := []struct {
		name     string
		words    int
		parallel int
		want     int
	}{
		{"one at a time keeps the size", 5000, 1, DefaultChunkWords},
		{"parallel spreads a long text", 5000, 4, 1250},
		{"parallel never makes pieces under the minimum", 300, 4, minParallelChunkWords},
		{"parallel never exceeds the size", 20000, 4, DefaultChunkWords},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAnalyzer(AnalyzerConfig{})
			if got := a.chunkSize(words(tt.words), tt.parallel); got != tt.want {
				t.Errorf("chunkSize = %d, want %d", got, tt.want)
			}
		})
	}
}

// llmFunc is an llms.Model made from a function.
type llmFunc func(ctx context.Context, msgs []llms.MessageContent, o ...llms.CallOption) (*llms.ContentResponse, error)

func (f llmFunc) GenerateContent(ctx context.Context, msgs []llms.MessageContent, o ...llms.CallOption) (*llms.ContentResponse, error) {
	return f(ctx, msgs, o...)
}

func (f llmFunc) Call(ctx context.Context, prompt string, o ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, f, prompt, o...)
}

func promptOf(msgs []llms.MessageContent) string {
	var b strings.Builder
	for _, m := range msgs {
		for _, part := range m.Parts {
			if t, ok := part.(llms.TextContent); ok {
				b.WriteString(t.Text)
			}
		}
	}
	return b.String()
}

func TestCheckInvalidReplyExplainsItself(t *testing.T) {
	tests := []struct {
		name       string
		stopReason string
		wantCut    bool
	}{
		{"stopped normally", "stop", false},
		{"provider does not say", "", false},
		{"output limit of OpenAI-compatible servers", "length", true},
		{"output limit of Anthropic", "max_tokens", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A reply cut in the middle of the JSON.
			fake := llmtest.New(llmtest.Reply{Text: `{"issues":[{"text":"He go","err`, StopReason: tt.stopReason})
			_, err := NewAnalyzer(AnalyzerConfig{}).Check(context.Background(), modelOf(fake), "He go home.")
			assertRoute(t, err, "CheckText->ParseResponse.LLMResponseInvalid")
			appErr, _ := errors.FromError(err)
			if got := appErr.Metadata["stop_reason"]; got != tt.stopReason {
				t.Errorf("stop_reason = %v, want %q", got, tt.stopReason)
			}
			if cut := strings.Contains(appErr.Message, "cut off"); cut != tt.wantCut {
				t.Errorf("message %q: says cut off = %v, want %v", appErr.Message, cut, tt.wantCut)
			}
			if strings.Contains(appErr.Message, "He go") || strings.Contains(strings.Join(metadataValues(appErr), " "), `"err`) {
				t.Error("the reply must not appear in the message or the metadata")
			}
		})
	}
}

func metadataValues(e *errors.Error) []string {
	var out []string
	for _, v := range e.Metadata {
		out = append(out, fmt.Sprint(v))
	}
	return out
}
