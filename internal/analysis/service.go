package analysis

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// Model is the LLM a request is sent to.
type Model struct {
	// Name is the display name of the configured LLM, used in messages and in
	// the usage records.
	Name        string
	LLM         llms.Model
	Temperature float64
	// Timeout overrides the default timeout of a request; 0 keeps the default.
	Timeout time.Duration
	// MaxParallel is how many requests of one check may run at once; 0 and 1
	// mean one at a time.
	MaxParallel int
}

// timeoutOr returns the model's timeout, or def when the model has none.
func (m Model) timeoutOr(def time.Duration) time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return def
}

// Chat binds the model's call to the Chat function used by CheckText.
func (m Model) Chat() Chat {
	return func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.Generate(ctx, m.LLM, req)
	}
}

// UsageKind says what a request was for.
type UsageKind string

// The kinds of request that use tokens. Test connection is not recorded.
const (
	KindCheck        UsageKind = "check"
	KindExplain      UsageKind = "explain"
	KindAlternatives UsageKind = "alternatives"
)

// UsageRecord is the token usage of one request, ready to be stored.
type UsageRecord struct {
	LLM   string
	Kind  UsageKind
	Usage llm.Usage
}

// UsageRecorder stores token usage. The store implements it (step 9). A
// failure to record never fails the request: it is logged and ignored.
type UsageRecorder interface {
	RecordUsage(ctx context.Context, rec UsageRecord) error
}

type noUsageRecorder struct{}

func (noUsageRecorder) RecordUsage(context.Context, UsageRecord) error { return nil }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// flight allows one request at a time: begin cancels the previous one.
type flight struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	seq    uint64
}

// begin cancels the running request, if any, and returns the context of the
// new one together with a function that must be called when it ends.
func (f *flight) begin(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	f.cancel = cancel
	f.seq++
	mine := f.seq
	f.mu.Unlock()
	return ctx, func() {
		f.mu.Lock()
		if f.seq == mine {
			f.cancel = nil
		}
		f.mu.Unlock()
		cancel()
	}
}

// stop cancels the running request, if any.
func (f *flight) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel != nil {
		f.cancel()
	}
}

// classifyError turns a failed request into the right outcome, in this order:
//
//  1. If the request's context was cancelled (by the user, by a newer request
//     or by shutdown), it is not an error: cancelled is true and err is nil.
//  2. An error from this package (such as LLMResponseInvalid) is returned as
//     it is, so its tag and stack are kept.
//  3. A deadline gives LLMCallTimeout.
//  4. Anything else gives LLMCallFailed.
//
// runCtx is the context of the whole operation, reqCtx the one of this request
// (it carries the timeout) and must not be cancelled yet.
func classifyError(runCtx, reqCtx context.Context, err error, name string, timeout time.Duration) (cancelled bool, out error) {
	if errors.Is(runCtx.Err(), context.Canceled) {
		return true, nil
	}
	if _, ok := errors.FromError(err); ok {
		return false, err
	}
	if errors.Is(reqCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return false, errors.NewErr("LLMCallTimeout", err).
			SetMetadata("llm", name, "timeout_s", int(timeout.Seconds())).
			SetMessage("The request to " + name + " took longer than " + timeout.String() + ".")
	}
	return false, errors.NewErr("LLMCallFailed", err).
		SetMetadata("llm", name).
		SetMessage("The request to " + name + " failed: " + err.Error())
}

func recordUsage(ctx context.Context, rec UsageRecorder, log *slog.Logger, name string, kind UsageKind, u llm.Usage) {
	if u.Total() == 0 {
		return
	}
	// A cancelled request still used the tokens it reports.
	err := rec.RecordUsage(context.WithoutCancel(ctx), UsageRecord{LLM: name, Kind: kind, Usage: u})
	if err != nil {
		log.Warn("saving token usage failed", "llm", name, "kind", string(kind), "error", err.Error())
	}
}
