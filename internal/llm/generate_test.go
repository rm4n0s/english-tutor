package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name       string
		reply      llmtest.Reply
		req        Request
		wantText   string
		wantUsage  Usage
		wantChunks []string
		wantStop   string
	}{
		{
			name:      "reported usage is used as it is",
			reply:     llmtest.Reply{Text: "  hello \n", Info: map[string]any{"PromptTokens": 12, "CompletionTokens": 3}},
			req:       Request{System: "sys", Prompt: "hi"},
			wantText:  "hello",
			wantUsage: Usage{InputTokens: 12, OutputTokens: 3},
		},
		{
			name:      "stop reason is passed on",
			reply:     llmtest.Reply{Text: "cut", StopReason: "length", Info: map[string]any{"PromptTokens": 1, "CompletionTokens": 1}},
			req:       Request{Prompt: "hi"},
			wantText:  "cut",
			wantUsage: Usage{InputTokens: 1, OutputTokens: 1},
			wantStop:  "length",
		},
		{
			name:      "missing usage is estimated from text length",
			reply:     llmtest.Reply{Text: "12345678"},
			req:       Request{System: "abcd", Prompt: "efgh"},
			wantText:  "12345678",
			wantUsage: Usage{InputTokens: 2, OutputTokens: 2, Estimated: true},
		},
		{
			name:       "streamed chunks reach the callback",
			reply:      llmtest.Reply{Chunks: []string{"Hel", "lo ", "there"}},
			req:        Request{Prompt: "hi"},
			wantText:   "Hello there",
			wantUsage:  Usage{InputTokens: 1, OutputTokens: 3, Estimated: true},
			wantChunks: []string{"Hel", "lo ", "there"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(tt.reply)
			var chunks []string
			if tt.wantChunks != nil {
				tt.req.OnChunk = func(c string) { chunks = append(chunks, c) }
			}
			tt.req.Temperature = 0.4

			got, err := Generate(context.Background(), fake, tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if got.StopReason != tt.wantStop {
				t.Errorf("StopReason = %q, want %q", got.StopReason, tt.wantStop)
			}
			if got.Text != tt.wantText || got.Usage != tt.wantUsage {
				t.Errorf("Generate() = %+v, want text %q usage %+v", got, tt.wantText, tt.wantUsage)
			}
			if strings.Join(chunks, "|") != strings.Join(tt.wantChunks, "|") {
				t.Errorf("chunks = %v, want %v", chunks, tt.wantChunks)
			}

			calls := fake.Calls()
			if len(calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(calls))
			}
			c := calls[0]
			if c.System != tt.req.System || c.Prompt != tt.req.Prompt {
				t.Errorf("model saw system %q prompt %q", c.System, c.Prompt)
			}
			if c.Options.Temperature != 0.4 {
				t.Errorf("temperature = %v, want 0.4", c.Options.Temperature)
			}
			if c.Streaming != (tt.wantChunks != nil) {
				t.Errorf("streaming = %v", c.Streaming)
			}
		})
	}
}

func TestGenerateNoChoices(t *testing.T) {
	// A model with no replies configured answers with an empty text.
	got, err := Generate(context.Background(), llmtest.New(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || !got.Usage.Estimated {
		t.Errorf("Generate() = %+v, want empty text and estimated usage", got)
	}
}

func TestGenerateErrors(t *testing.T) {
	boom := errors.New("Inner", "boom")
	ctxCancelled, cancel := context.WithCancel(context.Background())
	cancel()
	slowCtx, slowCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer slowCancel()

	tests := []struct {
		name string
		ctx  context.Context
		fake *llmtest.Fake
		want error
	}{
		{"model error is returned untouched", context.Background(), llmtest.New(llmtest.Reply{Err: boom}), boom},
		{"cancelled before the call", ctxCancelled, llmtest.New(llmtest.Reply{Text: "x"}), context.Canceled},
		{"cancelled while waiting", slowCtx, llmtest.New(llmtest.Reply{Text: "x", Delay: time.Second}), context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Generate(tt.ctx, tt.fake, Request{Prompt: "hi"})
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGenerateStopsWhenCancelledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := llmtest.New(llmtest.Reply{Chunks: []string{"a", "b", "c"}})
	var got []string
	_, err := Generate(ctx, fake, Request{Prompt: "hi", OnChunk: func(c string) {
		got = append(got, c)
		cancel()
	}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(got) != 1 {
		t.Errorf("chunks after cancel = %v, want only the first", got)
	}
}
