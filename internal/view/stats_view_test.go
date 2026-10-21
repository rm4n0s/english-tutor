package view

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/store"
)

func ts(id, category string, total, last30, last7 int, seen time.Time) store.TypeStat {
	return store.TypeStat{ErrorType: id, Category: category, Total: total, Last30: last30, Last7: last7, LastSeen: seen}
}

var day = func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

func sampleStats() []store.TypeStat {
	return []store.TypeStat{
		ts("article", "grammar", 10, 4, 1, day(14)),
		ts("spelling", "vocabulary", 6, 6, 3, day(15)),
		ts("word_order", "syntax", 2, 0, 0, day(1)),
		ts("verb_tense", "grammar", 6, 2, 0, day(10)),
	}
}

func ids(rows []store.TypeStat) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ErrorType
	}
	return out
}

func TestFilterStats(t *testing.T) {
	tests := []struct {
		name string
		f    StatsFilter
		want []string
	}{
		{"all time, all categories", StatsFilter{}, []string{"article", "spelling", "word_order", "verb_tense"}},
		{"30 days drops types with no recent mistake", StatsFilter{Period: store.Last30Days}, []string{"article", "spelling", "verb_tense"}},
		{"7 days", StatsFilter{Period: store.Last7Days}, []string{"article", "spelling"}},
		{"category", StatsFilter{Category: "grammar"}, []string{"article", "verb_tense"}},
		{"category and period", StatsFilter{Period: store.Last7Days, Category: "grammar"}, []string{"article"}},
		{"category without rows", StatsFilter{Category: "nothing"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(FilterStats(sampleStats(), tt.f)); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortStats(t *testing.T) {
	tests := []struct {
		name string
		sort StatsSort
		want []string
	}{
		{"default: total, highest first, ties by label", DefaultStatsSort, []string{"article", "spelling", "verb_tense", "word_order"}},
		{"total ascending", StatsSort{ColTotal, false}, []string{"word_order", "spelling", "verb_tense", "article"}},
		{"type alphabetical", StatsSort{ColType, false}, []string{"article", "spelling", "verb_tense", "word_order"}},
		{"type reversed", StatsSort{ColType, true}, []string{"word_order", "verb_tense", "spelling", "article"}},
		{"category", StatsSort{ColCategory, false}, []string{"article", "verb_tense", "word_order", "spelling"}},
		{"last 7 days", StatsSort{ColLast7, true}, []string{"spelling", "article", "verb_tense", "word_order"}},
		{"last 30 days", StatsSort{ColLast30, true}, []string{"spelling", "article", "verb_tense", "word_order"}},
		{"last seen, newest first", StatsSort{ColLastSeen, true}, []string{"spelling", "article", "verb_tense", "word_order"}},
		{"last seen, oldest first", StatsSort{ColLastSeen, false}, []string{"word_order", "verb_tense", "article", "spelling"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := sampleStats()
			SortStats(rows, tt.sort)
			if got := ids(rows); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortIsNumericNotTextual(t *testing.T) {
	rows := []store.TypeStat{ts("article", "grammar", 9, 0, 0, day(1)), ts("spelling", "vocabulary", 10, 0, 0, day(1))}
	SortStats(rows, DefaultStatsSort)
	if rows[0].ErrorType != "spelling" {
		t.Errorf("10 must sort above 9, got %v", ids(rows))
	}
}

func TestNextSort(t *testing.T) {
	tests := []struct {
		name    string
		cur     StatsSort
		clicked StatsColumn
		want    StatsSort
	}{
		{"same column reverses", StatsSort{ColTotal, true}, ColTotal, StatsSort{ColTotal, false}},
		{"and back", StatsSort{ColTotal, false}, ColTotal, StatsSort{ColTotal, true}},
		{"a number column starts with the largest", StatsSort{ColType, false}, ColLast7, StatsSort{ColLast7, true}},
		{"a text column starts alphabetical", DefaultStatsSort, ColType, StatsSort{ColType, false}},
		{"category starts alphabetical", DefaultStatsSort, ColCategory, StatsSort{ColCategory, false}},
		{"last seen starts with the newest", DefaultStatsSort, ColLastSeen, StatsSort{ColLastSeen, true}},
	}
	for _, tt := range tests {
		if got := NextSort(tt.cur, tt.clicked); got != tt.want {
			t.Errorf("%s: NextSort() = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestStatsCells(t *testing.T) {
	loc := time.Local
	defer func() { time.Local = loc }()
	time.Local = time.UTC
	got := StatsCells(ts("article", "grammar", 10, 4, 1, day(14)))
	want := [StatsColumnCount]string{"Article", "Grammar", "10", "1", "4", "2026-10-14"}
	if got != want {
		t.Errorf("cells = %v, want %v", got, want)
	}
	// An id this version does not know is shown as it is.
	if got := StatsCells(ts("future_type", "syntax", 1, 1, 1, time.Time{})); got[ColType] != "future_type" || got[ColLastSeen] != "" || got[ColCategory] != "Syntax" {
		t.Errorf("cells = %v", got)
	}
}

func TestStatsSummary(t *testing.T) {
	tests := []struct {
		name string
		f    StatsFilter
		want string
	}{
		{"all time", StatsFilter{}, "24 mistakes in all time. Most frequent: Article."},
		{"30 days", StatsFilter{Period: store.Last30Days}, "12 mistakes in the last 30 days. Most frequent: Spelling."},
		{"7 days, tie-free", StatsFilter{Period: store.Last7Days}, "4 mistakes in the last 7 days. Most frequent: Spelling."},
		{"category", StatsFilter{Category: "syntax"}, "2 mistakes in all time. Most frequent: Word order."},
		{"nothing", StatsFilter{Category: "none"}, "No mistakes recorded for all time."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatsSummary(sampleStats(), tt.f); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
	if got := StatsSummary([]store.TypeStat{ts("article", "grammar", 1, 1, 1, day(1))}, StatsFilter{}); got != "1 mistake in all time. Most frequent: Article." {
		t.Errorf("singular: %q", got)
	}
	if got := StatsSummary(nil, StatsFilter{Period: store.Last7Days}); got != "No mistakes recorded for the last 7 days." {
		t.Errorf("empty: %q", got)
	}
}

func TestFormatExample(t *testing.T) {
	loc := time.Local
	defer func() { time.Local = loc }()
	time.Local = time.UTC
	tests := []struct {
		e    store.Example
		want string
	}{
		{store.Example{Snippet: "he  go\nhome", CreatedAt: day(3)}, "he go home  (2026-10-03)"},
		{store.Example{Snippet: "teh"}, "teh"},
	}
	for _, tt := range tests {
		if got := FormatExample(tt.e); got != tt.want {
			t.Errorf("FormatExample() = %q, want %q", got, tt.want)
		}
	}
}

func TestUsageRows(t *testing.T) {
	all := []store.UsageTotal{
		{LLMName: "local", InputTokens: 100, OutputTokens: 10, Estimated: true},
		{LLMName: "claude", InputTokens: 5234, OutputTokens: 1100},
	}
	last30 := []store.UsageTotal{{LLMName: "claude", InputTokens: 800, OutputTokens: 90}}
	last7 := []store.UsageTotal{{LLMName: "claude", InputTokens: 12000, OutputTokens: 3}, {LLMName: "other", InputTokens: 1, OutputTokens: 1}}

	got := UsageRows(all, last30, last7)
	want := []UsageRow{
		{LLM: "claude", AllTime: "5.2k in / 1.1k out", Last30: "800 in / 90 out", Last7: "12k in / 3 out"},
		{LLM: "local", AllTime: "~100 in / ~10 out", Last30: "0 in / 0 out", Last7: "0 in / 0 out"},
		{LLM: "other", AllTime: "0 in / 0 out", Last30: "0 in / 0 out", Last7: "1 in / 1 out"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %+v\nwant   %+v", got, want)
	}
	if rows := UsageRows(nil, nil, nil); len(rows) != 0 {
		t.Errorf("no usage gives no rows, got %+v", rows)
	}
}

func TestPeriodLabels(t *testing.T) {
	want := map[store.Period]string{store.AllTime: "all time", store.Last30Days: "the last 30 days", store.Last7Days: "the last 7 days"}
	for p, w := range want {
		if got := PeriodLabel(p); got != w {
			t.Errorf("PeriodLabel(%d) = %q, want %q", p, got, w)
		}
	}
}
