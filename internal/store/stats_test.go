package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *fakeClock) daysAgo(n int) time.Time { return c.Now().AddDate(0, 0, -n) }

func newTestStats(t *testing.T) (*Stats, *fakeClock) {
	t.Helper()
	clock := newClock()
	return NewStats(newTestStore(t), clock.Now), clock
}

func mistake(errorType, category, snippet, fp string) Mistake {
	return Mistake{ErrorType: errorType, Category: category, Snippet: snippet, Fingerprint: fp}
}

func countEvents(t *testing.T, st *Stats) int {
	t.Helper()
	var n int64
	if err := st.store.db.Model(&ErrorEvent{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestRecord(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	ms := []Mistake{
		mistake("verb_tense", "grammar", "he go", "fp1"),
		mistake("spelling", "vocabulary", "teh", "fp2"),
	}

	res, err := st.Record(ctx, "session:a", "local", ms)
	if err != nil || res != (RecordResult{Inserted: 2}) {
		t.Fatalf("first Record() = %+v, %v", res, err)
	}

	// Re-checking the same text counts nothing new.
	res, err = st.Record(ctx, "session:a", "local", ms)
	if err != nil || res != (RecordResult{Duplicates: 2}) {
		t.Fatalf("second Record() = %+v, %v", res, err)
	}
	if n := countEvents(t, st); n != 2 {
		t.Errorf("events = %d, want 2", n)
	}

	// One new mistake among old ones.
	res, err = st.Record(ctx, "session:a", "local", append(ms, mistake("article", "grammar", "a apple", "fp3")))
	if err != nil || res != (RecordResult{Inserted: 1, Duplicates: 2}) {
		t.Fatalf("third Record() = %+v, %v", res, err)
	}

	// The same mistake in another text is counted again: repeated mistakes
	// count every time they are made.
	res, err = st.Record(ctx, "session:b", "local", ms)
	if err != nil || res.Inserted != 2 {
		t.Fatalf("other source Record() = %+v, %v", res, err)
	}
	if n := countEvents(t, st); n != 5 {
		t.Errorf("events = %d, want 5", n)
	}
}

func TestRecordKeepsTheFirstTypeAndTheFirstTime(t *testing.T) {
	ctx := context.Background()
	st, clock := newTestStats(t)
	if _, err := st.Record(ctx, "s", "llm-a", []Mistake{mistake("verb_tense", "grammar", "he go", "fp")}); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(time.Hour))
	res, err := st.Record(ctx, "s", "llm-b", []Mistake{mistake("verb_form", "grammar", "he go", "fp")})
	if err != nil || res.Duplicates != 1 {
		t.Fatalf("Record() = %+v, %v", res, err)
	}
	var ev ErrorEvent
	if err := st.store.db.First(&ev).Error; err != nil {
		t.Fatal(err)
	}
	if ev.ErrorType != "verb_tense" || ev.LLMName != "llm-a" || !ev.CreatedAt.Equal(clock.Now().Add(-time.Hour)) {
		t.Errorf("event = %+v, want the first type, LLM and time kept", ev)
	}
}

func TestRecordManyMistakesInBatches(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	var ms []Mistake
	for i := range 350 { // more than one batch
		ms = append(ms, mistake("article", "grammar", "x", "fp"+string(rune('A'+i%26))+string(rune('a'+i/26))))
	}
	res, err := st.Record(ctx, "s", "llm", ms)
	if err != nil || res.Inserted != 350 {
		t.Fatalf("Record() = %+v, %v", res, err)
	}
}

func TestRecordNothingDoesNotTouchTheDatabase(t *testing.T) {
	st := NewStats(closedStore(t), nil)
	if res, err := st.Record(context.Background(), "s", "llm", nil); err != nil || res != (RecordResult{}) {
		t.Errorf("Record(nil) = %+v, %v", res, err)
	}
}

func TestNotAnError(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	ms := []Mistake{mistake("article", "grammar", "a apple", "fp1"), mistake("spelling", "vocabulary", "teh", "fp2")}
	if _, err := st.Record(ctx, "s1", "llm", ms); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Record(ctx, "s2", "llm", ms); err != nil {
		t.Fatal(err)
	}

	if err := st.NotAnError(ctx, "s1", "fp1"); err != nil {
		t.Fatal(err)
	}
	if n := countEvents(t, st); n != 3 {
		t.Fatalf("events = %d, want 3 (only s1/fp1 removed)", n)
	}

	// A re-check of the same text does not bring it back; the rest is a duplicate.
	res, err := st.Record(ctx, "s1", "llm", ms)
	if err != nil || res != (RecordResult{Ignored: 1, Duplicates: 1}) {
		t.Fatalf("Record() = %+v, %v", res, err)
	}
	// The ignore set is per source: another text is not affected.
	if res, _ := st.Record(ctx, "s3", "llm", ms); res.Inserted != 2 {
		t.Errorf("other source Record() = %+v, want both inserted", res)
	}

	// Marking something that was never recorded is fine.
	if err := st.NotAnError(ctx, "s1", "never-recorded"); err != nil {
		t.Errorf("NotAnError() on a missing event: %v", err)
	}
}

func TestNotAnErrorWithEmptyKeysDeletesNothing(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	if _, err := st.Record(ctx, "s1", "llm", []Mistake{mistake("article", "grammar", "x", "fp1")}); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"", ""}, {"s1", ""}, {"", "fp1"}} {
		if err := st.NotAnError(ctx, c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	if n := countEvents(t, st); n != 1 {
		t.Errorf("events = %d, want 1: an empty key must not match everything", n)
	}
}

func TestMoveSource(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	if _, err := st.Record(ctx, "session:x", "llm", []Mistake{
		mistake("article", "grammar", "a", "fp1"),
		mistake("spelling", "vocabulary", "b", "fp2"),
		mistake("verb_tense", "grammar", "c", "fp3"),
	}); err != nil {
		t.Fatal(err)
	}
	// The destination already has fp2 (the text was saved there before).
	if _, err := st.Record(ctx, "file:/a.txt", "llm", []Mistake{mistake("spelling", "vocabulary", "b", "fp2")}); err != nil {
		t.Fatal(err)
	}
	if err := st.NotAnError(ctx, "session:x", "fp9"); err != nil {
		t.Fatal(err)
	}

	if err := st.MoveSource(ctx, "session:x", "file:/a.txt"); err != nil {
		t.Fatal(err)
	}

	var sources []string
	if err := st.store.db.Model(&ErrorEvent{}).Distinct().Pluck("source_key", &sources).Error; err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0] != "file:/a.txt" {
		t.Errorf("sources = %v, want only the new one", sources)
	}
	if n := countEvents(t, st); n != 3 {
		t.Errorf("events = %d, want 3: fp1, fp2 (once), fp3", n)
	}
	// Nothing is counted twice when the moved text is checked again.
	res, err := st.Record(ctx, "file:/a.txt", "llm", []Mistake{
		mistake("article", "grammar", "a", "fp1"), mistake("verb_tense", "grammar", "c", "fp3"),
		mistake("article", "grammar", "z", "fp9"),
	})
	if err != nil || res != (RecordResult{Duplicates: 2, Ignored: 1}) {
		t.Errorf("Record() after the move = %+v, %v", res, err)
	}
}

func TestMoveSourceEdgeCases(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	if _, err := st.Record(ctx, "a", "llm", []Mistake{mistake("article", "grammar", "x", "fp1")}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, from, to string }{
		{"same source", "a", "a"},
		{"source without events", "nothing", "b"},
	} {
		if err := st.MoveSource(ctx, tt.from, tt.to); err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	if n := countEvents(t, st); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func seedHistory(t *testing.T, st *Stats, clock *fakeClock) {
	t.Helper()
	ctx := context.Background()
	now := clock.Now()
	add := func(daysAgo int, errorType, category, snippet, fp string) {
		clock.Set(now.AddDate(0, 0, -daysAgo))
		if _, err := st.Record(ctx, "s", "llm", []Mistake{mistake(errorType, category, snippet, fp)}); err != nil {
			t.Fatal(err)
		}
	}
	add(40, "article", "grammar", "a apple", "a1")
	add(20, "article", "grammar", "a hour", "a2")
	add(7, "article", "grammar", "an dog", "a3") // exactly 7 days ago counts as inside the window
	add(3, "article", "grammar", "a umbrella", "a4")
	add(1, "spelling", "vocabulary", "teh", "s1")
	add(10, "spelling", "vocabulary", "recieve", "s2")
	add(0, "word_order", "syntax", "always I go", "w1")
	clock.Set(now)
}

func TestTypeStats(t *testing.T) {
	st, clock := newTestStats(t)
	seedHistory(t, st, clock)

	got, err := st.TypeStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []TypeStat{
		{ErrorType: "article", Category: "grammar", Total: 4, Last30: 3, Last7: 2, LastSeen: clock.daysAgo(3)},
		{ErrorType: "spelling", Category: "vocabulary", Total: 2, Last30: 2, Last7: 1, LastSeen: clock.daysAgo(1)},
		{ErrorType: "word_order", Category: "syntax", Total: 1, Last30: 1, Last7: 1, LastSeen: clock.daysAgo(0)},
	}
	if len(got) != len(want) {
		t.Fatalf("TypeStats() = %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ErrorType != w.ErrorType || g.Category != w.Category || g.Total != w.Total || g.Last30 != w.Last30 || g.Last7 != w.Last7 {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
		if !g.LastSeen.Equal(w.LastSeen) {
			t.Errorf("row %d LastSeen = %v, want %v", i, g.LastSeen, w.LastSeen)
		}
	}
}

func TestTypeStatsEmpty(t *testing.T) {
	st, _ := newTestStats(t)
	got, err := st.TypeStats(context.Background())
	if err != nil || len(got) != 0 {
		t.Errorf("TypeStats() = %+v, %v; want no rows", got, err)
	}
}

func TestTypeStatsTiesAreOrderedByType(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	if _, err := st.Record(ctx, "s", "llm", []Mistake{
		mistake("spelling", "vocabulary", "a", "1"), mistake("article", "grammar", "b", "2"),
		mistake("pronoun", "grammar", "c", "3"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.TypeStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{got[0].ErrorType, got[1].ErrorType, got[2].ErrorType}
	if order[0] != "article" || order[1] != "pronoun" || order[2] != "spelling" {
		t.Errorf("order = %v", order)
	}
}

func TestCountForType(t *testing.T) {
	st, clock := newTestStats(t)
	seedHistory(t, st, clock)
	tests := []struct {
		errorType string
		want      TypeCount
	}{
		{"article", TypeCount{Total: 4, Last30: 3, Last7: 2}},
		{"spelling", TypeCount{Total: 2, Last30: 2, Last7: 1}},
		{"preposition", TypeCount{}},
		{"", TypeCount{}},
	}
	for _, tt := range tests {
		got, err := st.CountForType(context.Background(), tt.errorType)
		if err != nil || got != tt.want {
			t.Errorf("CountForType(%q) = %+v, %v; want %+v", tt.errorType, got, err, tt.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	st, clock := newTestStats(t)
	seedHistory(t, st, clock)
	rows, err := st.TypeStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		period   Period
		category string
		want     Summary
	}{
		{"all time", AllTime, "", Summary{Total: 7, MostFrequent: "article"}},
		{"30 days", Last30Days, "", Summary{Total: 6, MostFrequent: "article"}},
		{"7 days", Last7Days, "", Summary{Total: 4, MostFrequent: "article"}},
		{"vocabulary only", AllTime, "vocabulary", Summary{Total: 2, MostFrequent: "spelling"}},
		{"category without mistakes", AllTime, "nothing", Summary{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summarize(rows, tt.period, tt.category); got != tt.want {
				t.Errorf("Summarize() = %+v, want %+v", got, tt.want)
			}
		})
	}
	if got := Summarize(nil, AllTime, ""); got != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v", got)
	}
	// A tie goes to the error type that sorts first.
	tied := []TypeStat{{ErrorType: "spelling", Category: "vocabulary", Total: 2}, {ErrorType: "article", Category: "grammar", Total: 2}}
	if got := Summarize(tied, AllTime, ""); got.MostFrequent != "article" {
		t.Errorf("tie = %+v", got)
	}
}

func TestRecentExamples(t *testing.T) {
	st, clock := newTestStats(t)
	seedHistory(t, st, clock)
	got, err := st.RecentExamples(context.Background(), "article", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a umbrella", "an dog", "a hour"}
	if len(got) != 3 {
		t.Fatalf("examples = %+v", got)
	}
	for i := range want {
		if got[i].Snippet != want[i] {
			t.Errorf("example %d = %q, want %q", i, got[i].Snippet, want[i])
		}
	}
	if none, err := st.RecentExamples(context.Background(), "pronoun", 3); err != nil || len(none) != 0 {
		t.Errorf("RecentExamples(pronoun) = %+v, %v", none, err)
	}
}

func TestResetMistakesKeepsTheIgnoreSet(t *testing.T) {
	ctx := context.Background()
	st, clock := newTestStats(t)
	seedHistory(t, st, clock)
	if err := st.NotAnError(ctx, "s", "w1"); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetMistakes(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countEvents(t, st); n != 0 {
		t.Fatalf("events = %d after reset", n)
	}
	res, err := st.Record(ctx, "s", "llm", []Mistake{mistake("word_order", "syntax", "x", "w1"), mistake("article", "grammar", "y", "new")})
	if err != nil || res != (RecordResult{Inserted: 1, Ignored: 1}) {
		t.Errorf("Record() = %+v, %v", res, err)
	}
	// Resetting an empty table is fine.
	if err := st.ResetMistakes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetMistakes(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRecordUsageAndTotals(t *testing.T) {
	ctx := context.Background()
	st, clock := newTestStats(t)
	now := clock.Now()
	add := func(daysAgo int, name string, kind analysis.UsageKind, in, out int, estimated bool) {
		clock.Set(now.AddDate(0, 0, -daysAgo))
		err := st.RecordUsage(ctx, analysis.UsageRecord{LLM: name, Kind: kind, Usage: llm.Usage{InputTokens: in, OutputTokens: out, Estimated: estimated}})
		if err != nil {
			t.Fatal(err)
		}
	}
	add(40, "claude", analysis.KindCheck, 1000, 100, false)
	add(20, "claude", analysis.KindExplain, 200, 50, false)
	add(2, "claude", analysis.KindCheck, 300, 30, false)
	add(2, "local", analysis.KindCheck, 80, 8, true)
	add(1, "local", analysis.KindExplain, 20, 2, false)
	add(1, "local", analysis.KindAlternatives, 5, 1, false)
	clock.Set(now)

	tests := []struct {
		name   string
		period Period
		want   []UsageTotal
	}{
		{"all time", AllTime, []UsageTotal{
			{LLMName: "claude", InputTokens: 1500, OutputTokens: 180},
			{LLMName: "local", InputTokens: 105, OutputTokens: 11, Estimated: true},
		}},
		{"30 days", Last30Days, []UsageTotal{
			{LLMName: "claude", InputTokens: 500, OutputTokens: 80},
			{LLMName: "local", InputTokens: 105, OutputTokens: 11, Estimated: true},
		}},
		{"7 days", Last7Days, []UsageTotal{
			{LLMName: "claude", InputTokens: 300, OutputTokens: 30},
			{LLMName: "local", InputTokens: 105, OutputTokens: 11, Estimated: true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.UsageTotals(ctx, tt.period)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("UsageTotals() = %+v", got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}

	if err := st.ResetUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := st.UsageTotals(ctx, AllTime); err != nil || len(got) != 0 {
		t.Errorf("after reset UsageTotals() = %+v, %v", got, err)
	}
}

func TestStatsConcurrentRecording(t *testing.T) {
	ctx := context.Background()
	st, _ := newTestStats(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms := []Mistake{mistake("article", "grammar", "x", "shared"), mistake("article", "grammar", "y", "own"+string(rune('a'+i)))}
			if _, err := st.Record(ctx, "s", "llm", ms); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := countEvents(t, st); n != 9 {
		t.Errorf("events = %d, want 9: one shared plus eight own", n)
	}
}

func TestStatsFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		run   func(st *Stats) error
		route string
	}{
		{"record", func(st *Stats) error {
			_, err := st.Record(ctx, "s", "llm", []Mistake{mistake("article", "grammar", "x", "f")})
			return err
		}, "Stats.Record.ErrorEventSaveFailed"},
		{"move", func(st *Stats) error { return st.MoveSource(ctx, "a", "b") }, "Stats.MoveSource.ErrorEventSaveFailed"},
		{"not an error", func(st *Stats) error { return st.NotAnError(ctx, "s", "f") }, "Stats.NotAnError.StatsResetFailed"},
		{"reset mistakes", func(st *Stats) error { return st.ResetMistakes(ctx) }, "Stats.ResetMistakes.StatsResetFailed"},
		{"reset usage", func(st *Stats) error { return st.ResetUsage(ctx) }, "Stats.ResetUsage.StatsResetFailed"},
		{"type stats", func(st *Stats) error { _, err := st.TypeStats(ctx); return err }, "Stats.TypeStats->Stats.groupedCounts.StatsQueryFailed"},
		{"count for type", func(st *Stats) error { _, err := st.CountForType(ctx, "article"); return err }, "Stats.CountForType.StatsQueryFailed"},
		{"recent examples", func(st *Stats) error { _, err := st.RecentExamples(ctx, "article", 3); return err }, "Stats.RecentExamples.StatsQueryFailed"},
		{"usage totals", func(st *Stats) error { _, err := st.UsageTotals(ctx, AllTime); return err }, "Stats.UsageTotals.StatsQueryFailed"},
		{"record usage", func(st *Stats) error {
			return st.RecordUsage(ctx, analysis.UsageRecord{LLM: "x", Kind: analysis.KindCheck, Usage: llm.Usage{InputTokens: 1}})
		}, "Stats.RecordUsage.UsageSaveFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRoute(t, tt.run(NewStats(closedStore(t), nil)), tt.route)
		})
	}
}

func TestPeriodSince(t *testing.T) {
	now := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		p      Period
		want   time.Time
		wantOK bool
	}{
		{AllTime, time.Time{}, false},
		{Last7Days, time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC), true},
		{Last30Days, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		got, ok := tt.p.since(now)
		if ok != tt.wantOK || !got.Equal(tt.want) {
			t.Errorf("Period(%d).since = %v, %v; want %v, %v", tt.p, got, ok, tt.want, tt.wantOK)
		}
	}
}
