package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

func chatFor(f *llmtest.Fake) Chat {
	return func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.Generate(ctx, f, req)
	}
}

func TestCheckText(t *testing.T) {
	boom := errors.New("Inner", "network down")
	usage := map[string]any{"PromptTokens": 100, "CompletionTokens": 20}
	tests := []struct {
		name        string
		replies     []llmtest.Reply
		wantIssues  int
		wantRetried bool
		wantCalls   int
		wantUsage   llm.Usage
		wantRoute   string
		wantErr     error
	}{
		{
			name:       "valid first reply",
			replies:    []llmtest.Reply{{Text: oneIssue, Info: usage}},
			wantIssues: 1, wantCalls: 1, wantUsage: llm.Usage{InputTokens: 100, OutputTokens: 20},
		},
		{
			name:       "no errors found",
			replies:    []llmtest.Reply{{Text: `{"issues": []}`, Info: usage}},
			wantIssues: 0, wantCalls: 1, wantUsage: llm.Usage{InputTokens: 100, OutputTokens: 20},
		},
		{
			name:       "invalid then valid retries once and sums usage",
			replies:    []llmtest.Reply{{Text: "Sorry, I cannot.", Info: usage}, {Text: oneIssue, Info: usage}},
			wantIssues: 1, wantRetried: true, wantCalls: 2, wantUsage: llm.Usage{InputTokens: 200, OutputTokens: 40},
		},
		{
			name:        "invalid twice gives LLMResponseInvalid",
			replies:     []llmtest.Reply{{Text: "nope", Info: usage}, {Text: "still nope", Info: usage}},
			wantRetried: true, wantCalls: 2, wantUsage: llm.Usage{InputTokens: 200, OutputTokens: 40},
			wantRoute: "CheckText->ParseResponse.LLMResponseInvalid",
		},
		{
			name:      "request error is returned untouched and not retried",
			replies:   []llmtest.Reply{{Err: boom}},
			wantCalls: 1, wantErr: boom,
		},
		{
			name:        "request error on the retry is returned untouched",
			replies:     []llmtest.Reply{{Text: "nope", Info: usage}, {Err: boom}},
			wantRetried: true, wantCalls: 2, wantErr: boom, wantUsage: llm.Usage{InputTokens: 100, OutputTokens: 20},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := llmtest.New(tt.replies...)
			got, err := CheckText(context.Background(), chatFor(fake), "He go to school.", 0.3)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantRoute != "":
				appErr, ok := errors.FromError(err)
				if !ok {
					t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
				}
				if !appErr.HasRoute(tt.wantRoute) {
					t.Errorf("unexpected failure path: %s", appErr.Route())
				}
			case err != nil:
				t.Fatal(err)
			}
			if len(got.Issues) != tt.wantIssues {
				t.Errorf("issues = %d, want %d", len(got.Issues), tt.wantIssues)
			}
			if got.Retried != tt.wantRetried {
				t.Errorf("Retried = %v, want %v", got.Retried, tt.wantRetried)
			}
			if got.Usage != tt.wantUsage {
				t.Errorf("Usage = %+v, want %+v", got.Usage, tt.wantUsage)
			}
			calls := fake.Calls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", len(calls), tt.wantCalls)
			}
			for _, c := range calls {
				if c.Options.Temperature != 0.3 {
					t.Errorf("temperature = %v, want 0.3", c.Options.Temperature)
				}
				if !strings.Contains(c.Prompt, "He go to school.") {
					t.Error("the text is missing from the prompt")
				}
			}
			if tt.wantCalls == 2 {
				if strings.Contains(calls[0].Prompt, "not valid JSON") || !strings.Contains(calls[1].Prompt, "not valid JSON") {
					t.Error("only the second request should carry the reminder")
				}
			}
		})
	}
}

func TestCheckTextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := llmtest.New(llmtest.Reply{Text: oneIssue})
	_, err := CheckText(ctx, chatFor(fake), "text", 0.2)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if n := len(fake.Calls()); n != 1 {
		t.Errorf("calls = %d, want 1 (a cancelled check is not retried)", n)
	}
}
