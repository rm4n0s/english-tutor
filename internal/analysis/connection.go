package analysis

import (
	"context"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// DefaultTestTimeout is how long Test connection waits for the answer.
const DefaultTestTimeout = 30 * time.Second

// testPrompt is the tiny request of Test connection.
const testPrompt = "Reply with the single word: ok"

// TestResult is the outcome of TestConnection.
type TestResult struct {
	// Elapsed is how long the LLM took to answer.
	Elapsed time.Duration
	// Cancelled is true if the context was cancelled; that is not an error.
	Cancelled bool
}

// TestConnection builds the model for cfg and sends it a tiny prompt, to find
// out whether the configuration works. name is the LLM's display name, used in
// messages. The request is not recorded in the token usage (section 5.8).
//
// Errors are those of llm.New (LLMConfigInvalid, UnknownProvider,
// LLMClientCreateFailed) and of a request (LLMCallFailed, LLMCallTimeout). Their
// messages are meant to be shown to the user.
func TestConnection(ctx context.Context, name string, cfg llm.ModelConfig, timeout time.Duration) (TestResult, error) {
	if timeout <= 0 {
		timeout = DefaultTestTimeout
	}
	model, err := llm.New(cfg)
	if err != nil {
		return TestResult{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	started := time.Now()
	_, err = llm.Generate(reqCtx, model, llm.Request{Prompt: testPrompt, Temperature: cfg.Temperature})
	var (
		cancelled bool
		cerr      error
	)
	if err != nil {
		cancelled, cerr = classifyError(ctx, reqCtx, err, name, timeout)
	}
	cancel()

	switch {
	case cancelled:
		return TestResult{Cancelled: true}, nil
	case cerr != nil:
		return TestResult{}, cerr
	}
	return TestResult{Elapsed: time.Since(started)}, nil
}
