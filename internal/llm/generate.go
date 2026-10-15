package llm

import (
	"context"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// Request is one call to a model.
type Request struct {
	// System is the system prompt. It may be empty.
	System string
	// Prompt is the user message.
	Prompt string
	// Temperature is passed as a call option.
	Temperature float64
	// OnChunk, if set, makes the call stream and receives each text chunk.
	OnChunk func(chunk string)
}

// Response is the reply of one call.
type Response struct {
	Text  string
	Usage Usage
}

// Generate sends req to model and returns the reply with its token usage.
// Usage comes from the provider when it reports it and is estimated from the
// text length otherwise. Errors from the model are returned as they are, so
// the caller can tell a cancelled context (context.Canceled) from a failure.
func Generate(ctx context.Context, model llms.Model, req Request) (Response, error) {
	messages := make([]llms.MessageContent, 0, 2)
	if req.System != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeSystem, req.System))
	}
	messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, req.Prompt))

	opts := []llms.CallOption{llms.WithTemperature(req.Temperature)}
	if req.OnChunk != nil {
		opts = append(opts, llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			req.OnChunk(string(chunk))
			return nil
		}))
	}

	resp, err := model.GenerateContent(ctx, messages, opts...)
	if err != nil {
		return Response{}, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		// An empty answer is not a transport failure; the caller decides what
		// it means (the analysis layer rejects it as an invalid response).
		return Response{Usage: EstimateUsage(req.System+req.Prompt, "")}, nil
	}
	choice := resp.Choices[0]
	usage, ok := UsageFromInfo(choice.GenerationInfo)
	if !ok {
		usage = EstimateUsage(req.System+req.Prompt, choice.Content)
	}
	return Response{Text: strings.TrimSpace(choice.Content), Usage: usage}, nil
}
