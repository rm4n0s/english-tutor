package ui

import (
	"context"
	"github.com/rm4n0s/english-tutor/internal/view"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/uitest"
)

type fakeStats struct {
	rows     []store.TypeStat
	examples map[string][]store.Example
	usage    map[store.Period][]store.UsageTotal

	asked        []string
	mistakeReset int
	usageReset   int

	typeErr, exampleErr, resetErr, usageErr, resetUsageErr error
}

func (f *fakeStats) TypeStats(context.Context) ([]store.TypeStat, error) {
	return slices.Clone(f.rows), f.typeErr
}

func (f *fakeStats) RecentExamples(_ context.Context, id string, limit int) ([]store.Example, error) {
	f.asked = append(f.asked, id)
	ex := f.examples[id]
	return ex[:min(limit, len(ex))], f.exampleErr
}

func (f *fakeStats) ResetMistakes(context.Context) error {
	if f.resetErr != nil {
		return f.resetErr
	}
	f.mistakeReset++
	f.rows = nil
	return nil
}

func (f *fakeStats) UsageTotals(_ context.Context, p store.Period) ([]store.UsageTotal, error) {
	return f.usage[p], f.usageErr
}

func (f *fakeStats) ResetUsage(context.Context) error {
	if f.resetUsageErr != nil {
		return f.resetUsageErr
	}
	f.usageReset++
	f.usage = nil
	return nil
}

func newFakeStats() *fakeStats {
	return &fakeStats{
		rows: sampleStats(),
		examples: map[string][]store.Example{
			"article": {
				{Snippet: "a apple", CreatedAt: day(14)}, {Snippet: "an dog", CreatedAt: day(12)},
				{Snippet: "a hour", CreatedAt: day(10)}, {Snippet: "a umbrella", CreatedAt: day(2)},
			},
		},
		usage: map[store.Period][]store.UsageTotal{
			store.AllTime:    {{LLMName: "claude", InputTokens: 5234, OutputTokens: 1100}},
			store.Last30Days: {{LLMName: "claude", InputTokens: 800, OutputTokens: 90}},
		},
	}
}

func withStats(t *testing.T, b *fakeStats, f func(d *StatisticsDialog, resets *int, failed *[]error)) {
	t.Helper()
	uitest.OnUI(func() {
		resets := 0
		var failed []error
		d := NewStatisticsDialog(context.Background(), nil, b, StatisticsHooks{
			Reset:  func() { resets++ },
			Failed: func(err error) { failed = append(failed, err) },
		})
		defer d.dlg.Delete()
		d.confirm = func(string) bool { return true }
		f(d, &resets, &failed)
	})
}

func column(d *StatisticsDialog, c view.StatsColumn) []string {
	var out []string
	for r := range d.table.RowCount() {
		out = append(out, d.table.Item(r, int(c)).Text())
	}
	return out
}

func TestStatisticsShowsTheMistakesTable(t *testing.T) {
	withStats(t, newFakeStats(), func(d *StatisticsDialog, _ *int, _ *[]error) {
		if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling", "Verb tense", "Word order"}) {
			t.Errorf("types = %v: sorted by Total, highest first", got)
		}
		if got := column(d, view.ColTotal); !slices.Equal(got, []string{"10", "6", "6", "2"}) {
			t.Errorf("totals = %v", got)
		}
		if got := column(d, view.ColCategory); !slices.Equal(got, []string{"Grammar", "Vocabulary", "Grammar", "Syntax"}) {
			t.Errorf("categories = %v", got)
		}
		if d.summary.Text() != "24 mistakes in all time. Most frequent: Article." {
			t.Errorf("summary = %q", d.summary.Text())
		}
		headers := []string{}
		for c := range int(view.StatsColumnCount) {
			headers = append(headers, d.table.HorizontalHeaderItem(c).Text())
		}
		if !slices.Equal(headers, []string{"Error type", "Category", "Total", "Last 7 days", "Last 30 days", "Last seen"}) {
			t.Errorf("headers = %v", headers)
		}
	})
}

func TestStatisticsFilters(t *testing.T) {
	withStats(t, newFakeStats(), func(d *StatisticsDialog, _ *int, _ *[]error) {
		d.period.SetCurrentIndex(2) // last 7 days
		if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling"}) {
			t.Errorf("7 days: %v", got)
		}
		if d.summary.Text() != "4 mistakes in the last 7 days. Most frequent: Spelling." {
			t.Errorf("summary = %q", d.summary.Text())
		}
		d.category.SetCurrentIndex(1) // grammar
		if got := column(d, view.ColType); !slices.Equal(got, []string{"Article"}) {
			t.Errorf("7 days, grammar: %v", got)
		}
		d.period.SetCurrentIndex(0)
		d.category.SetCurrentIndex(0)
		if len(column(d, view.ColType)) != 4 {
			t.Error("clearing the filters shows every row again")
		}
	})
}

func TestStatisticsSortByClickingAHeader(t *testing.T) {
	withStats(t, newFakeStats(), func(d *StatisticsDialog, _ *int, _ *[]error) {
		header := d.table.HorizontalHeader()
		header.SectionClicked(int(view.ColType))
		if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling", "Verb tense", "Word order"}) {
			t.Errorf("type ascending: %v", got)
		}
		header.SectionClicked(int(view.ColType))
		if got := column(d, view.ColType); !slices.Equal(got, []string{"Word order", "Verb tense", "Spelling", "Article"}) {
			t.Errorf("type descending: %v", got)
		}
		header.SectionClicked(int(view.ColLast7))
		if got := column(d, view.ColType); got[0] != "Spelling" {
			t.Errorf("last 7 days, most first: %v", got)
		}
		header.SectionClicked(int(view.ColLastSeen))
		if got := column(d, view.ColType); got[0] != "Spelling" || got[len(got)-1] != "Word order" {
			t.Errorf("last seen, newest first: %v", got)
		}
		if d.sort != (view.StatsSort{Column: view.ColLastSeen, Descending: true}) {
			t.Errorf("sort = %+v", d.sort)
		}
	})
}

func TestStatisticsShowsExamplesOfTheSelectedRow(t *testing.T) {
	b := newFakeStats()
	withStats(t, b, func(d *StatisticsDialog, _ *int, _ *[]error) {
		if d.examples.Count() != 0 {
			t.Error("no row is selected, so no examples")
		}
		d.table.SetCurrentCell(0, 0) // Article
		if d.examples.Count() != 3 {
			t.Fatalf("examples = %d, want the three most recent", d.examples.Count())
		}
		got := []string{d.examples.Item(0).Text(), d.examples.Item(1).Text(), d.examples.Item(2).Text()}
		if !strings.HasPrefix(got[0], "a apple  (") || !strings.HasPrefix(got[2], "a hour  (") {
			t.Errorf("examples = %v", got)
		}
		d.table.SetCurrentCell(1, 0) // Spelling: none stored in the fake
		if d.examples.Count() != 0 || !slices.Equal(b.asked, []string{"article", "spelling"}) {
			t.Errorf("examples %d, asked %v", d.examples.Count(), b.asked)
		}
		// Changing the filter clears the examples (the selected row is gone).
		d.period.SetCurrentIndex(1)
		if d.examples.Count() != 0 {
			t.Error("a redraw clears the examples")
		}
	})
}

func TestStatisticsReset(t *testing.T) {
	b := newFakeStats()
	withStats(t, b, func(d *StatisticsDialog, resets *int, _ *[]error) {
		d.confirm = func(string) bool { return false }
		d.resetBtn.Click()
		if b.mistakeReset != 0 || *resets != 0 || d.table.RowCount() != 4 {
			t.Error("declining the confirmation must change nothing")
		}
		asked := ""
		d.confirm = func(text string) bool { asked = text; return true }
		d.resetBtn.Click()
		if !strings.Contains(asked, "Delete every recorded mistake") || b.mistakeReset != 1 || *resets != 1 {
			t.Errorf("asked %q, resets %d, hook %d", asked, b.mistakeReset, *resets)
		}
		if d.table.RowCount() != 0 || d.summary.Text() != "No mistakes recorded for all time." {
			t.Errorf("after the reset: %d rows, summary %q", d.table.RowCount(), d.summary.Text())
		}
	})
}

func TestStatisticsEmpty(t *testing.T) {
	b := newFakeStats()
	b.rows, b.usage = nil, nil
	withStats(t, b, func(d *StatisticsDialog, _ *int, failed *[]error) {
		if d.table.RowCount() != 0 || d.usageTable.RowCount() != 0 || len(*failed) != 0 {
			t.Errorf("rows %d, usage rows %d, failures %v", d.table.RowCount(), d.usageTable.RowCount(), *failed)
		}
		if d.summary.Text() != "No mistakes recorded for all time." {
			t.Errorf("summary = %q", d.summary.Text())
		}
	})
}

func TestTokenUsageTab(t *testing.T) {
	b := newFakeStats()
	withStats(t, b, func(d *StatisticsDialog, _ *int, _ *[]error) {
		if d.usageTable.RowCount() != 1 {
			t.Fatalf("usage rows = %d", d.usageTable.RowCount())
		}
		var cells []string
		for c := range 4 {
			cells = append(cells, d.usageTable.Item(0, c).Text())
		}
		want := []string{"claude", "5.2k in / 1.1k out", "800 in / 90 out", "0 in / 0 out"}
		if !slices.Equal(cells, want) {
			t.Errorf("cells = %v, want %v", cells, want)
		}

		d.confirm = func(string) bool { return false }
		d.resetUsage.Click()
		if b.usageReset != 0 || d.usageTable.RowCount() != 1 {
			t.Error("declining must change nothing")
		}
		d.confirm = func(string) bool { return true }
		d.resetUsage.Click()
		if b.usageReset != 1 || d.usageTable.RowCount() != 0 {
			t.Errorf("reset %d, rows %d", b.usageReset, d.usageTable.RowCount())
		}
		if d.table.RowCount() != 4 {
			t.Error("resetting the token usage leaves the mistakes alone")
		}
	})
}

func TestStatisticsErrorsShowABanner(t *testing.T) {
	tests := []struct {
		name  string
		setup func(b *fakeStats)
		act   func(d *StatisticsDialog)
	}{
		{"loading mistakes", func(b *fakeStats) { b.typeErr = errors.New("StatsQueryFailed", "database is locked") }, func(*StatisticsDialog) {}},
		{"loading usage", func(b *fakeStats) { b.usageErr = errors.New("StatsQueryFailed", "database is locked") }, func(*StatisticsDialog) {}},
		{"examples", func(b *fakeStats) { b.exampleErr = errors.New("StatsQueryFailed", "database is locked") }, func(d *StatisticsDialog) { d.table.SetCurrentCell(0, 0) }},
		{"reset mistakes", func(b *fakeStats) { b.resetErr = errors.New("StatsResetFailed", "database is locked") }, func(d *StatisticsDialog) { d.resetBtn.Click() }},
		{"reset usage", func(b *fakeStats) { b.resetUsageErr = errors.New("StatsResetFailed", "database is locked") }, func(d *StatisticsDialog) { d.resetUsage.Click() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newFakeStats()
			tt.setup(b)
			withStats(t, b, func(d *StatisticsDialog, resets *int, failed *[]error) {
				tt.act(d)
				if d.banner.IsHidden() || !strings.Contains(d.banner.Text(), "database is locked") || len(*failed) == 0 {
					t.Errorf("banner hidden=%v text=%q failures=%d", d.banner.IsHidden(), d.banner.Text(), len(*failed))
				}
				if *resets != 0 {
					t.Error("a failed reset must not tell the application that it happened")
				}
			})
		})
	}
}

func TestStatisticsKeepsRowsAfterAFailedReset(t *testing.T) {
	b := newFakeStats()
	b.resetErr = errors.New("StatsResetFailed", "nope")
	withStats(t, b, func(d *StatisticsDialog, _ *int, _ *[]error) {
		d.resetBtn.Click()
		if d.table.RowCount() != 4 {
			t.Errorf("rows = %d: a failed reset leaves the table as it was", d.table.RowCount())
		}
	})
}

func ts(id, category string, total, last30, last7 int, seen time.Time) store.TypeStat {
	return store.TypeStat{ErrorType: id, Category: category, Total: total, Last30: last30, Last7: last7, LastSeen: seen}
}

func sampleStats() []store.TypeStat {
	return []store.TypeStat{
		ts("article", "grammar", 10, 4, 1, day(14)),
		ts("spelling", "vocabulary", 6, 6, 3, day(15)),
		ts("word_order", "syntax", 2, 0, 0, day(1)),
		ts("verb_tense", "grammar", 6, 2, 0, day(10)),
	}
}
