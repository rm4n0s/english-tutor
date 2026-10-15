package analysis

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
)

type recorder struct {
	mu   sync.Mutex
	recs []UsageRecord
	err  error
}

func (r *recorder) RecordUsage(_ context.Context, rec UsageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec)
	return r.err
}

func (r *recorder) records() []UsageRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]UsageRecord(nil), r.recs...)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func logTo(b *syncBuffer) *slog.Logger { return slog.New(slog.NewTextHandler(b, nil)) }

func modelOf(f *llmtest.Fake) Model { return Model{Name: "local", LLM: f, Temperature: 0.2} }

func usageInfo(in, out int) map[string]any {
	return map[string]any{"PromptTokens": in, "CompletionTokens": out}
}

func issuesJSON(texts ...string) string {
	var parts []string
	for _, t := range texts {
		parts = append(parts, `{"text":"`+t+`","error_type":"verb_form","message":"m"}`)
	}
	return `{"issues":[` + strings.Join(parts, ",") + `]}`
}

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with route %q, got nil", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// waitForCalls blocks until the fake has received n calls.
func waitForCalls(t *testing.T, f *llmtest.Fake, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(f.Calls()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("fake received %d calls, want %d", len(f.Calls()), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCheck(t *testing.T) {
	text := "He go home. She have a cat."
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON("He go", "She have"), Info: usageInfo(50, 10)})
	rec := &recorder{}
	a := NewAnalyzer(AnalyzerConfig{Usage: rec})

	res, err := a.Check(context.Background(), modelOf(fake), text)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cancelled || res.Chunks != 1 || len(res.Placed) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if got := text[res.Placed[0].Start:res.Placed[0].End]; got != "He go" {
		t.Errorf("first span = %q", got)
	}
	if got := text[res.Placed[1].Start:res.Placed[1].End]; got != "She have" {
		t.Errorf("second span = %q", got)
	}
	if res.Usage != (llm.Usage{InputTokens: 50, OutputTokens: 10}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	want := []UsageRecord{{LLM: "local", Kind: KindCheck, Usage: llm.Usage{InputTokens: 50, OutputTokens: 10}}}
	if got := rec.records(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("recorded = %+v, want %+v", got, want)
	}
}

func TestCheckMultipleChunksShiftsOffsets(t *testing.T) {
	text := "He go home today.\n\nShe have a cat.\n\nThey is happy."
	fake := llmtest.New(
		llmtest.Reply{Text: issuesJSON("He go"), Info: usageInfo(10, 2)},
		llmtest.Reply{Text: issuesJSON("She have"), Info: usageInfo(11, 3)},
		llmtest.Reply{Text: issuesJSON("They is"), Info: usageInfo(12, 4)},
	)
	rec := &recorder{}
	a := NewAnalyzer(AnalyzerConfig{ChunkWords: 4, Usage: rec})

	res, err := a.Check(context.Background(), modelOf(fake), text)
	if err != nil {
		t.Fatal(err)
	}
	if res.Chunks != 3 || len(res.Placed) != 3 {
		t.Fatalf("result = %+v", res)
	}
	for i, want := range []string{"He go", "She have", "They is"} {
		p := res.Placed[i]
		if got := text[p.Start:p.End]; got != want {
			t.Errorf("span %d = %q, want %q", i, got, want)
		}
	}
	if res.Usage != (llm.Usage{InputTokens: 33, OutputTokens: 9}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	if n := len(rec.records()); n != 3 {
		t.Errorf("usage records = %d, want one per chunk", n)
	}
	// Each chunk's prompt carries only that chunk's text.
	calls := fake.Calls()
	if strings.Contains(calls[0].Prompt, "They is") || !strings.Contains(calls[2].Prompt, "They is") {
		t.Error("chunks were not sent separately")
	}
}

func TestCheckCountsUnplacedIssues(t *testing.T) {
	text := "He go home."
	reply := `{"issues":[{"text":"He go","error_type":"verb_form"},{"text":"not in the text","error_type":"article"},{"text":"","error_type":"article"}]}`
	a := NewAnalyzer(AnalyzerConfig{})
	res, err := a.Check(context.Background(), modelOf(llmtest.New(llmtest.Reply{Text: reply})), text)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Placed) != 1 || res.Unplaced != 2 {
		t.Errorf("placed %d, unplaced %d; want 1 and 2", len(res.Placed), res.Unplaced)
	}
}

func TestCheckEmptyTextMakesNoRequest(t *testing.T) {
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON()})
	res, err := NewAnalyzer(AnalyzerConfig{}).Check(context.Background(), modelOf(fake), " \n ")
	if err != nil || res.Chunks != 0 || len(res.Placed) != 0 || len(fake.Calls()) != 0 {
		t.Errorf("res = %+v, err = %v, calls = %d", res, err, len(fake.Calls()))
	}
}

func TestCheckFailures(t *testing.T) {
	boom := io.ErrUnexpectedEOF // a plain error, like the ones the HTTP clients return
	tests := []struct {
		name      string
		reply     llmtest.Reply
		timeout   time.Duration
		route     string
		wantInMsg string
		wantUsage int // records
	}{
		{
			name:  "request failure",
			reply: llmtest.Reply{Err: boom}, route: "Analyzer.Check->classifyError.LLMCallFailed",
			wantInMsg: "local",
		},
		{
			name:    "timeout",
			reply:   llmtest.Reply{Text: issuesJSON(), Delay: 5 * time.Second},
			timeout: 30 * time.Millisecond,
			route:   "Analyzer.Check->classifyError.LLMCallTimeout", wantInMsg: "took longer",
		},
		{
			name:  "invalid reply twice",
			reply: llmtest.Reply{Text: "no json here", Info: usageInfo(5, 5)}, route: "Analyzer.Check->CheckText->ParseResponse.LLMResponseInvalid",
			wantUsage: 1, // both requests of the chunk, summed
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			a := NewAnalyzer(AnalyzerConfig{Timeout: tt.timeout, Usage: rec})
			res, err := a.Check(context.Background(), modelOf(llmtest.New(tt.reply)), "He go home.")
			assertRoute(t, err, tt.route)
			if res.Cancelled {
				t.Error("a failure is not a cancellation")
			}
			if appErr, _ := errors.FromError(err); !strings.Contains(appErr.Message, tt.wantInMsg) {
				t.Errorf("message %q should contain %q", appErr.Message, tt.wantInMsg)
			}
			if n := len(rec.records()); n != tt.wantUsage {
				t.Errorf("usage records = %d, want %d", n, tt.wantUsage)
			}
		})
	}
}

func TestCheckDeadlineOfParentIsATimeoutNotACancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON(), Delay: 5 * time.Second})
	_, err := NewAnalyzer(AnalyzerConfig{}).Check(ctx, modelOf(fake), "He go home.")
	assertRoute(t, err, "classifyError.LLMCallTimeout")
}

func TestCheckCancelIsNotAnError(t *testing.T) {
	logs := &syncBuffer{}
	rec := &recorder{}
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON("He go"), Delay: 5 * time.Second})
	a := NewAnalyzer(AnalyzerConfig{Usage: rec, Logger: logTo(logs)})

	type out struct {
		res CheckResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := a.Check(context.Background(), modelOf(fake), "He go home.")
		done <- out{res, err}
	}()
	waitForCalls(t, fake, 1)
	a.Cancel()

	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("a cancelled check must not return an error, got %v", o.err)
		}
		if !o.res.Cancelled || len(o.res.Placed) != 0 {
			t.Errorf("result = %+v, want cancelled with no issues", o.res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check did not return after Cancel")
	}
	if len(rec.records()) != 0 {
		t.Errorf("no usage was reported, but %+v was recorded", rec.records())
	}
	if strings.Contains(logs.String(), "level=ERROR") || strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("a cancelled check must not log above info:\n%s", logs.String())
	}
	// Cancel with nothing running is harmless.
	a.Cancel()
}

func TestCheckParentContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := NewAnalyzer(AnalyzerConfig{}).Check(ctx, modelOf(llmtest.New(llmtest.Reply{Text: issuesJSON()})), "He go home.")
	if err != nil || !res.Cancelled {
		t.Errorf("res = %+v, err = %v; want cancelled, no error", res, err)
	}
}

func TestCheckNewCheckCancelsThePreviousOne(t *testing.T) {
	fake := llmtest.New(
		llmtest.Reply{Text: issuesJSON("He go"), Delay: 5 * time.Second},
		llmtest.Reply{Text: issuesJSON("She have")},
	)
	a := NewAnalyzer(AnalyzerConfig{})
	first := make(chan CheckResult, 1)
	go func() {
		res, err := a.Check(context.Background(), modelOf(fake), "He go home.")
		if err != nil {
			t.Errorf("first check: %v", err)
		}
		first <- res
	}()
	waitForCalls(t, fake, 1)

	second, err := a.Check(context.Background(), modelOf(fake), "She have a cat.")
	if err != nil || second.Cancelled || len(second.Placed) != 1 {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
	select {
	case res := <-first:
		if !res.Cancelled {
			t.Errorf("first check should be cancelled, got %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first check did not end")
	}
}

func TestCheckRecorderFailureDoesNotFailTheCheck(t *testing.T) {
	logs := &syncBuffer{}
	rec := &recorder{err: errors.New("UsageSaveFailed", "disk full")}
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON("He go"), Info: usageInfo(5, 5)})
	res, err := NewAnalyzer(AnalyzerConfig{Usage: rec, Logger: logTo(logs)}).Check(context.Background(), modelOf(fake), "He go home.")
	if err != nil || len(res.Placed) != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if !strings.Contains(logs.String(), "saving token usage failed") {
		t.Errorf("the failure should be logged:\n%s", logs.String())
	}
}

func TestCheckLogsNeverContainTheTextOrReply(t *testing.T) {
	logs := &syncBuffer{}
	text := "My secret essay about purple elephants go home."
	fake := llmtest.New(llmtest.Reply{Text: issuesJSON("elephants go"), Info: usageInfo(5, 5)})
	if _, err := NewAnalyzer(AnalyzerConfig{Logger: logTo(logs)}).Check(context.Background(), modelOf(fake), text); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, "check finished") || !strings.Contains(out, "input_tokens=5") {
		t.Errorf("expected a summary with token counts:\n%s", out)
	}
	for _, secret := range []string{"purple", "elephants", "essay"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains user text %q:\n%s", secret, out)
		}
	}
}
