// Package llmtest provides a fake langchaingo model for tests. No test ever
// calls a real provider.
package llmtest

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// Reply describes how the fake answers one call.
type Reply struct {
	// Text is the full answer. If Chunks is set, the answer is their
	// concatenation and Text is ignored.
	Text string
	// Chunks are delivered one by one to the streaming function, if any.
	Chunks []string
	// Err, if set, is returned instead of an answer (after Delay).
	Err error
	// Delay is waited before answering; the wait ends early, with the
	// context's error, if the context is done.
	Delay time.Duration
	// Info becomes the choice's GenerationInfo (token counts).
	Info map[string]any
}

// Call records one call made to the fake.
type Call struct {
	System    string
	Prompt    string
	Options   llms.CallOptions
	Streaming bool
}

// Fake is a llms.Model that plays back Replies in order. When the replies run
// out, the last one is repeated. It is safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	replies []Reply
	calls   []Call
}

// New returns a Fake that plays back replies. With no replies it answers with
// an empty text.
func New(replies ...Reply) *Fake {
	return &Fake{replies: replies}
}

// Calls returns a copy of the calls made so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// GenerateContent implements llms.Model.
func (f *Fake) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	var opts llms.CallOptions
	for _, o := range options {
		o(&opts)
	}
	call := Call{Options: opts, Streaming: opts.StreamingFunc != nil}
	for _, m := range messages {
		var text strings.Builder
		for _, p := range m.Parts {
			if t, ok := p.(llms.TextContent); ok {
				text.WriteString(t.Text)
			}
		}
		if m.Role == llms.ChatMessageTypeSystem {
			call.System += text.String()
		} else {
			call.Prompt += text.String()
		}
	}

	f.mu.Lock()
	f.calls = append(f.calls, call)
	var reply Reply
	if len(f.replies) > 0 {
		i := min(len(f.calls)-1, len(f.replies)-1)
		reply = f.replies[i]
	}
	f.mu.Unlock()

	if reply.Delay > 0 {
		timer := time.NewTimer(reply.Delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reply.Err != nil {
		return nil, reply.Err
	}

	text := reply.Text
	if reply.Chunks != nil {
		text = strings.Join(reply.Chunks, "")
	}
	if opts.StreamingFunc != nil {
		chunks := reply.Chunks
		if chunks == nil {
			chunks = []string{reply.Text}
		}
		for _, c := range chunks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := opts.StreamingFunc(ctx, []byte(c)); err != nil {
				return nil, err
			}
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content:        text,
		GenerationInfo: reply.Info,
	}}}, nil
}

// Call implements llms.Model.
func (f *Fake) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, f, prompt, options...)
}
