package gui

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
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

func ts(id, category string, total, last30, last7 int, seen int) store.TypeStat {
	return store.TypeStat{ErrorType: id, Category: category, Total: total, Last30: last30, Last7: last7, LastSeen: day(seen)}
}

func newFakeStats() *fakeStats {
	return &fakeStats{
		rows: []store.TypeStat{
			ts("article", "grammar", 10, 4, 1, 14),
			ts("spelling", "vocabulary", 6, 6, 3, 15),
			ts("word_order", "syntax", 2, 0, 0, 1),
			ts("verb_tense", "grammar", 6, 2, 0, 10),
		},
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

func showStats(t *testing.T, b *fakeStats) (*gluttertest.Tester, *StatisticsDialog, *int, *[]error) {
	t.Helper()
	tester, w := pumpWindow(t)
	resets := 0
	var failed []error
	d := NewStatisticsDialog(context.Background(), w, b, StatisticsHooks{
		Reset:  func() { resets++ },
		Failed: func(err error) { failed = append(failed, err) },
	})
	d.confirm = func(_ string, done func(bool)) { done(true) }
	w.ShowDialog(d)
	tester.PumpAndSettle()
	return tester, d, &resets, &failed
}

func column(d *StatisticsDialog, c view.StatsColumn) []string {
	var out []string
	for _, row := range d.Cells() {
		out = append(out, row[c])
	}
	return out
}

func TestStatisticsShowsTheMistakesTable(t *testing.T) {
	tester, d, _, _ := showStats(t, newFakeStats())
	if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling", "Verb tense", "Word order"}) {
		t.Errorf("types = %v: sorted by Total, highest first", got)
	}
	if got := column(d, view.ColTotal); !slices.Equal(got, []string{"10", "6", "6", "2"}) {
		t.Errorf("totals = %v", got)
	}
	if got := d.Summary(); got != "24 mistakes in all time. Most frequent: Article." {
		t.Errorf("summary = %q", got)
	}
	for _, text := range []string{"Statistics", "Mistakes", "Token usage", "Article", "Word order", "Reset statistics", "Close", d.Summary()} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	for c := range view.StatsColumnCount {
		title := view.StatsColumnTitles[c]
		if err := gluttertest.FindsWidgets(tester, gluttertest.Descendant(gluttertest.ByType[*m.Dialog](), gluttertest.ByTextContaining(title))); err != nil {
			t.Errorf("header %q: %v", title, err)
		}
	}
}

func TestStatisticsFilterAndSort(t *testing.T) {
	tester, d, _, _ := showStats(t, newFakeStats())
	tester.Tap(gluttertest.First(inDialog("Vocabulary"))) // the filter, not the column of the table
	tester.Pump()
	if got := column(d, view.ColType); !slices.Equal(got, []string{"Spelling"}) {
		t.Errorf("Vocabulary shows %v", got)
	}
	d.SetCategory(0)
	d.SetPeriod(2) // last 7 days hides the types with no mistake in it
	if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling"}) {
		t.Errorf("last 7 days shows %v", got)
	}
	d.SetPeriod(0)
	d.ClickHeader(int(view.ColType))
	if got := column(d, view.ColType); !slices.Equal(got, []string{"Article", "Spelling", "Verb tense", "Word order"}) && d.Sort().Column != view.ColType {
		t.Errorf("sort by type: %v %+v", got, d.Sort())
	}
	first := d.Sort()
	d.ClickHeader(int(view.ColType))
	if d.Sort() == first {
		t.Error("clicking the same header again reverses the order")
	}
	d.SetCategory(99) // ignored
	d.ClickHeader(-1)
}

func TestStatisticsSelectingARowShowsItsExamples(t *testing.T) {
	b := newFakeStats()
	tester, d, _, _ := showStats(t, b)
	tester.Tap(gluttertest.Descendant(gluttertest.ByType[*m.Dialog](), gluttertest.ByText("Article")))
	tester.Pump()
	if got := d.Examples(); len(got) != 3 || !slices.Equal(b.asked, []string{"article"}) {
		t.Errorf("examples %v asked %v", got, b.asked)
	}
	if err := gluttertest.FindsWidgets(tester, gluttertest.ByTextContaining("a apple")); err != nil {
		t.Error(err)
	}
	d.SelectRow(-1)
	if len(d.Examples()) != 0 {
		t.Error("no row, no examples")
	}
	d.SelectRow(99)
}

func TestStatisticsReset(t *testing.T) {
	b := newFakeStats()
	tester, d, resets, _ := showStats(t, b)
	var asked string
	d.confirm = func(text string, done func(bool)) { asked = text; done(false) }
	tapInDialog(t, tester, "Reset statistics")
	if b.mistakeReset != 0 || asked == "" {
		t.Errorf("declining reset %d, asked %q", b.mistakeReset, asked)
	}
	d.confirm = func(_ string, done func(bool)) { done(true) }
	tapInDialog(t, tester, "Reset statistics")
	if b.mistakeReset != 1 || *resets != 1 || len(d.Cells()) != 0 {
		t.Errorf("reset %d hook %d rows %v", b.mistakeReset, *resets, d.Cells())
	}
}

func TestStatisticsTokenUsageTab(t *testing.T) {
	b := newFakeStats()
	tester, d, _, _ := showStats(t, b)
	tapInDialog(t, tester, "Token usage")
	cells := d.UsageCells()
	if len(cells) != 1 || cells[0][0] != "claude" || cells[0][1] != "5.2k in / 1.1k out" || cells[0][3] != "0 in / 0 out" {
		t.Errorf("usage = %v", cells)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("claude")); err != nil {
		t.Error(err)
	}
	tapInDialog(t, tester, "Reset token usage")
	if b.usageReset != 1 || len(d.UsageCells()) != 0 {
		t.Errorf("reset %d usage %v", b.usageReset, d.UsageCells())
	}
}

func TestStatisticsErrorsAreShownAndReported(t *testing.T) {
	b := newFakeStats()
	b.typeErr = errors.New("StatsQueryFailed", "The statistics could not be read.")
	b.usageErr = errors.New("StatsQueryFailed", "The usage could not be read.")
	_, d, _, failed := showStats(t, b)
	if d.BannerText() == "" || len(*failed) != 2 {
		t.Errorf("banner %q failed %d", d.BannerText(), len(*failed))
	}

	b = newFakeStats()
	b.resetErr = errors.New("StatsResetFailed", "The statistics could not be reset.")
	b.resetUsageErr = errors.New("StatsResetFailed", "The usage could not be reset.")
	b.exampleErr = errors.New("StatsQueryFailed", "The examples could not be read.")
	_, d, resets, failed := showStats(t, b)
	d.ResetMistakesClick()
	if d.BannerText() != "The statistics could not be reset." || *resets != 0 || len(d.Cells()) != 4 {
		t.Errorf("banner %q resets %d rows %d", d.BannerText(), *resets, len(d.Cells()))
	}
	d.ResetUsageClick()
	if d.BannerText() != "The usage could not be reset." {
		t.Errorf("banner %q", d.BannerText())
	}
	d.SelectRow(0)
	if d.BannerText() != "The examples could not be read." || len(*failed) != 3 {
		t.Errorf("banner %q failed %d", d.BannerText(), len(*failed))
	}
}
