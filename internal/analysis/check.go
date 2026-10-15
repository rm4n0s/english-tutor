package analysis

import (
	"context"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// Chat sends one request to a model and returns its reply and token usage. It
// is llm.Generate bound to a model; tests pass a fake.
type Chat func(ctx context.Context, req llm.Request) (llm.Response, error)

// ChunkResult is the outcome of checking one piece of text.
type ChunkResult struct {
	Parsed
	// Usage is the total of every request made, including a retry.
	Usage llm.Usage
	// Retried is true if the first reply was invalid and a second request was
	// made.
	Retried bool
}

// CheckText asks the model for the errors in text and parses the reply. If the
// first reply is not valid, it asks once more with a reminder to return JSON
// only; a second invalid reply gives LLMResponseInvalid. An error from chat is
// returned untouched and is never retried, so the caller can tell a cancelled
// context from a failed request.
func CheckText(ctx context.Context, chat Chat, text string, temperature float64) (ChunkResult, error) {
	prompt := BuildCheckPrompt(text)
	var res ChunkResult

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := chat(ctx, llm.Request{
			System:      prompt.System,
			Prompt:      prompt.User,
			Temperature: temperature,
		})
		if err != nil {
			return res, err
		}
		res.Usage = res.Usage.Add(resp.Usage)

		parsed, perr := ParseResponse(resp.Text)
		if perr == nil {
			res.Parsed = parsed
			return res, nil
		}
		if attempt == 1 {
			return res, perr
		}
		res.Retried = true
		prompt = prompt.WithRetryReminder()
	}
	return res, nil // unreachable: the loop returns on its last pass
}
