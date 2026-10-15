package analysis

import (
	"context"
	"log/slog"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// Default request timeouts (section 6 of the spec).
const (
	DefaultCheckTimeout   = 60 * time.Second
	DefaultExplainTimeout = 120 * time.Second
)

// AnalyzerConfig configures an Analyzer. Zero values mean the defaults.
type AnalyzerConfig struct {
	// Timeout applies to each request of a check (one per chunk).
	Timeout time.Duration
	// ChunkWords is the largest number of words sent in one request.
	ChunkWords int
	Usage      UsageRecorder
	Logger     *slog.Logger
}

// Analyzer runs checks. Only one check runs at a time: starting a check
// cancels the one before it.
type Analyzer struct {
	timeout    time.Duration
	chunkWords int
	usage      UsageRecorder
	log        *slog.Logger
	flight     flight
}

// NewAnalyzer returns an Analyzer using cfg.
func NewAnalyzer(cfg AnalyzerConfig) *Analyzer {
	a := &Analyzer{timeout: cfg.Timeout, chunkWords: cfg.ChunkWords, usage: cfg.Usage, log: cfg.Logger}
	if a.timeout <= 0 {
		a.timeout = DefaultCheckTimeout
	}
	if a.chunkWords <= 0 {
		a.chunkWords = DefaultChunkWords
	}
	if a.usage == nil {
		a.usage = noUsageRecorder{}
	}
	if a.log == nil {
		a.log = discardLogger()
	}
	return a
}

// CheckResult is the outcome of a check.
type CheckResult struct {
	// LocateResult holds the placed issues (byte offsets into the checked
	// text) and the counts of issues that were not placed.
	LocateResult
	// Usage is the total token usage of the check.
	Usage llm.Usage
	// Chunks is the number of pieces the text was split into.
	Chunks int
	// Cancelled is true if the check was cancelled by Cancel, by a newer
	// check or by the context. Cancelling is not an error; no issues are
	// returned in that case.
	Cancelled bool
}

// Check sends text to the model in chunks and returns the issues found, placed
// in the text. Errors are LLMCallFailed, LLMCallTimeout or LLMResponseInvalid;
// a cancelled check returns Cancelled and no error (section 5.1). Token usage
// is recorded for every request that reports it, even if the check later fails
// or is cancelled.
func (a *Analyzer) Check(ctx context.Context, m Model, text string) (CheckResult, error) {
	runCtx, done := a.flight.begin(ctx)
	defer done()
	started := time.Now()

	chunks := SplitChunks(text, a.chunkWords)
	res := CheckResult{Chunks: len(chunks)}
	for _, c := range chunks {
		reqCtx, cancel := context.WithTimeout(runCtx, a.timeout)
		cr, err := CheckText(reqCtx, m.Chat(), c.Text, m.Temperature)
		// classifyError looks at reqCtx, so cancel it only afterwards.
		var (
			cancelled bool
			cerr      error
		)
		if err != nil {
			cancelled, cerr = classifyError(runCtx, reqCtx, err, m.Name, a.timeout)
		}
		cancel()

		res.Usage = res.Usage.Add(cr.Usage)
		recordUsage(ctx, a.usage, a.log, m.Name, KindCheck, cr.Usage)
		if cancelled {
			a.log.Info("check cancelled", "llm", m.Name, "chunks", len(chunks), "elapsed_ms", time.Since(started).Milliseconds())
			return CheckResult{Usage: res.Usage, Chunks: res.Chunks, Cancelled: true}, nil
		}
		if cerr != nil {
			return CheckResult{Usage: res.Usage, Chunks: res.Chunks}, cerr
		}

		located := Locate(c.Text, cr.Issues)
		located.Unplaced += cr.Dropped
		res.Append(located, c.Start)
	}
	a.log.Info("check finished",
		"llm", m.Name, "chunks", len(chunks), "issues", len(res.Placed),
		"unplaced", res.Unplaced, "overlapping", res.Overlapping,
		"input_tokens", res.Usage.InputTokens, "output_tokens", res.Usage.OutputTokens,
		"estimated", res.Usage.Estimated, "elapsed_ms", time.Since(started).Milliseconds())
	return res, nil
}

// Cancel cancels the running check, if any. The Check call returns a result
// with Cancelled set.
func (a *Analyzer) Cancel() { a.flight.stop() }
