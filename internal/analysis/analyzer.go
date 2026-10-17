package analysis

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// Default request timeouts (section 6 of the spec).
const (
	DefaultCheckTimeout   = 15 * time.Minute
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

// minParallelChunkWords is the smallest chunk made to spread a text over
// parallel requests: smaller pieces cost more in repeated prompt than they save
// in waiting.
const minParallelChunkWords = 200

// chunkSize returns the largest chunk, in words, for text. With one request at
// a time it is the configured size. With several, the text is spread over them
// (but never into pieces under minParallelChunkWords), so a short essay still
// uses the parallel requests.
func (a *Analyzer) chunkSize(text string, parallel int) int {
	if parallel <= 1 {
		return a.chunkWords
	}
	perRequest := (countWords(text) + parallel - 1) / parallel
	return min(a.chunkWords, max(perRequest, minParallelChunkWords))
}

// Check sends text to the model in chunks and returns the issues found, placed
// in the text. Up to m.MaxParallel chunks are requested at once. Errors are
// LLMCallFailed, LLMCallTimeout or LLMResponseInvalid; the first failure
// cancels the requests still running and is the one returned. A cancelled
// check returns Cancelled and no error (section 5.1). Token usage is recorded
// for every request that reports it, even if the check later fails or is
// cancelled.
func (a *Analyzer) Check(ctx context.Context, m Model, text string) (CheckResult, error) {
	runCtx, done := a.flight.begin(ctx)
	defer done()
	started := time.Now()

	timeout := m.timeoutOr(a.timeout)
	parallel := max(m.MaxParallel, 1)
	chunks := SplitChunks(text, a.chunkSize(text, parallel))
	res := CheckResult{Chunks: len(chunks)}

	// stopCtx ends when the check is cancelled or when one chunk has failed.
	stopCtx, stop := context.WithCancel(runCtx)
	defer stop()
	var (
		mu        sync.Mutex // guards the three variables below
		usage     llm.Usage
		firstErr  error
		cancelled bool
		located   = make([]LocateResult, len(chunks)) // one slot per chunk, written by one worker
		slots     = make(chan struct{}, parallel)
		wg        sync.WaitGroup
	)
	// run checks one chunk and files the outcome. With one request at a time it
	// runs in this goroutine, so failures keep a short route.
	run := func(i int, c Chunk) {
		cr, wasCancelled, cerr := a.checkChunk(ctx, stopCtx, m, timeout, c)
		mu.Lock()
		defer mu.Unlock()
		usage = usage.Add(cr.Usage)
		switch {
		case wasCancelled:
			// Cancelled by the user, or by the failure of another chunk.
			cancelled = true
		case cerr != nil:
			if firstErr == nil {
				firstErr = cerr
			}
			stop()
		default:
			located[i] = cr.LocateResult
		}
	}
	for i, c := range chunks {
		// A slot frees up as soon as a request ends, which cancelling makes quick.
		slots <- struct{}{}
		if errors.Is(stopCtx.Err(), context.Canceled) {
			mu.Lock()
			cancelled = true // a chunk was skipped
			mu.Unlock()
			break
		}
		if parallel == 1 {
			run(i, c)
			<-slots
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			run(i, c)
		}()
	}
	wg.Wait()

	res.Usage = usage
	switch {
	case firstErr != nil:
		return CheckResult{Usage: usage, Chunks: res.Chunks}, firstErr
	case cancelled:
		a.log.Info("check cancelled", "llm", m.Name, "chunks", len(chunks), "elapsed_ms", time.Since(started).Milliseconds())
		return CheckResult{Usage: usage, Chunks: res.Chunks, Cancelled: true}, nil
	}
	for i, c := range chunks {
		res.Append(located[i], c.Start)
	}
	a.log.Info("check finished",
		"llm", m.Name, "chunks", len(chunks), "parallel", parallel, "issues", len(res.Placed),
		"unplaced", res.Unplaced, "overlapping", res.Overlapping,
		"input_tokens", res.Usage.InputTokens, "output_tokens", res.Usage.OutputTokens,
		"estimated", res.Usage.Estimated, "elapsed_ms", time.Since(started).Milliseconds())
	return res, nil
}

// checkChunk sends one chunk and locates its issues. It records the usage of
// the request. cancelled is true when stopCtx ended (it is then not an error).
func (a *Analyzer) checkChunk(ctx, stopCtx context.Context, m Model, timeout time.Duration, c Chunk) (cr CheckResult, cancelled bool, err error) {
	reqCtx, cancel := context.WithTimeout(stopCtx, timeout)
	res, err := CheckText(reqCtx, m.Chat(), c.Text, m.Temperature)
	if err != nil {
		// classifyError looks at reqCtx, so cancel it only afterwards.
		cancelled, err = classifyError(stopCtx, reqCtx, err, m.Name, timeout)
	}
	cancel()

	recordUsage(ctx, a.usage, a.log, m.Name, KindCheck, res.Usage)
	cr.Usage = res.Usage
	if cancelled || err != nil {
		return cr, cancelled, err
	}
	cr.LocateResult = Locate(c.Text, res.Issues)
	cr.Unplaced += res.Dropped
	return cr, false, nil
}

// Cancel cancels the running check, if any. The Check call returns a result
// with Cancelled set.
func (a *Analyzer) Cancel() { a.flight.stop() }
