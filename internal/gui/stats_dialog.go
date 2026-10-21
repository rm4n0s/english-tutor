package gui

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
)

// StatisticsBackend is what the Statistics dialog needs from the rest of the
// application (section 4.6). *store.Stats implements it.
//
// The queries are small grouped counts over a local file and run on the UI
// thread while the dialog opens or after a reset, like the Preferences dialog's
// reads.
type StatisticsBackend interface {
	TypeStats(ctx context.Context) ([]store.TypeStat, error)
	RecentExamples(ctx context.Context, errorType string, limit int) ([]store.Example, error)
	ResetMistakes(ctx context.Context) error
	UsageTotals(ctx context.Context, p store.Period) ([]store.UsageTotal, error)
	ResetUsage(ctx context.Context) error
}

// StatisticsHooks lets the application react to the dialog.
type StatisticsHooks struct {
	// Reset is called after the recorded mistakes were deleted.
	Reset func()
	// Failed is called with every error the dialog shows, so it can be logged.
	Failed func(err error)
}

// examplesShown is how many examples a selected row shows (section 4.6).
const examplesShown = 3

// StatisticsDialog is the Statistics dialog (section 4.6): a table of the user's
// mistakes by error type and a table of token usage.
type StatisticsDialog struct {
	dialogBase
	ctx     context.Context
	backend StatisticsBackend
	hooks   StatisticsHooks

	rows   []store.TypeStat // every row, unfiltered
	shown  []store.TypeStat // rows after the filter and the sort
	filter view.StatsFilter
	sort   view.StatsSort

	tab      int // 0 mistakes, 1 token usage
	selected int // row of shown, -1 for none
	examples []string
	usage    []view.UsageRow
	banner   string

	// confirm asks before a reset; tests replace it.
	confirm func(text string, done func(yes bool))
}

var statsCategoryChoices = []struct{ label, value string }{
	{"All categories", ""},
	{analysis.Grammar.Label(), string(analysis.Grammar)},
	{analysis.Vocabulary.Label(), string(analysis.Vocabulary)},
	{analysis.Syntax.Label(), string(analysis.Syntax)},
}

var statsPeriodChoices = []struct {
	label  string
	period store.Period
}{
	{"All time", store.AllTime},
	{"Last 30 days", store.Last30Days},
	{"Last 7 days", store.Last7Days},
}

// NewStatisticsDialog builds the dialog and loads the statistics into it. Show it
// with win.ShowDialog.
func NewStatisticsDialog(ctx context.Context, win *MainWindow, backend StatisticsBackend, hooks StatisticsHooks) *StatisticsDialog {
	d := &StatisticsDialog{dialogBase: newDialogBase(), ctx: ctx, backend: backend, hooks: hooks,
		sort: view.DefaultStatsSort, selected: -1}
	d.confirm = func(text string, done func(bool)) { win.Confirm("Statistics", text, done) }
	d.reload()
	return d
}

// Dismissed marks the dialog as gone.
func (d *StatisticsDialog) Dismissed() { d.open = false }

// reload reads everything from the backend and redraws both tables.
func (d *StatisticsDialog) reload() {
	rows, err := d.backend.TypeStats(d.ctx)
	if err != nil {
		d.fail(err)
		rows = nil
	}
	d.rows = rows
	d.apply()
	d.loadUsage()
}

// apply applies the filter and the sort to the loaded rows.
func (d *StatisticsDialog) apply() {
	d.shown = view.FilterStats(d.rows, d.filter)
	view.SortStats(d.shown, d.sort)
	d.selected, d.examples = -1, nil
	d.refresh()
}

func (d *StatisticsDialog) loadUsage() {
	var lists [3][]store.UsageTotal
	for i, p := range []store.Period{store.AllTime, store.Last30Days, store.Last7Days} {
		list, err := d.backend.UsageTotals(d.ctx, p)
		if err != nil {
			d.fail(err)
			return
		}
		lists[i] = list
	}
	d.usage = view.UsageRows(lists[0], lists[1], lists[2])
	d.refresh()
}

// SelectRow selects a row of the mistakes table, as a click would, and shows
// the examples of its error type.
func (d *StatisticsDialog) SelectRow(row int) {
	d.selected, d.examples = -1, nil
	if row >= 0 && row < len(d.shown) {
		d.selected = row
		examples, err := d.backend.RecentExamples(d.ctx, d.shown[row].ErrorType, examplesShown)
		if err != nil {
			d.fail(err)
		}
		for _, e := range examples {
			d.examples = append(d.examples, view.FormatExample(e))
		}
	}
	d.refresh()
}

// SetCategory picks an entry of the category filter.
func (d *StatisticsDialog) SetCategory(i int) {
	if i >= 0 && i < len(statsCategoryChoices) {
		d.filter.Category = statsCategoryChoices[i].value
		d.apply()
	}
}

// SetPeriod picks an entry of the period filter.
func (d *StatisticsDialog) SetPeriod(i int) {
	if i >= 0 && i < len(statsPeriodChoices) {
		d.filter.Period = statsPeriodChoices[i].period
		d.apply()
	}
}

// ClickHeader sorts by a column, as clicking its title does.
func (d *StatisticsDialog) ClickHeader(col int) {
	if col >= 0 && col < int(view.StatsColumnCount) {
		d.sort = view.NextSort(d.sort, view.StatsColumn(col))
		d.apply()
	}
}

// Sort returns how the table is sorted.
func (d *StatisticsDialog) Sort() view.StatsSort { return d.sort }

// SelectTab shows the Mistakes tab (0) or the Token usage tab (1).
func (d *StatisticsDialog) SelectTab(i int) { d.tab = min(max(i, 0), 1); d.refresh() }

// Summary returns the line above the table.
func (d *StatisticsDialog) Summary() string { return view.StatsSummary(d.rows, d.filter) }

// Cells returns the text of the mistakes table, row by row.
func (d *StatisticsDialog) Cells() [][]string {
	out := make([][]string, len(d.shown))
	for i, r := range d.shown {
		cells := view.StatsCells(r)
		out[i] = cells[:]
	}
	return out
}

// Examples returns the examples of the selected row.
func (d *StatisticsDialog) Examples() []string { return append([]string(nil), d.examples...) }

// UsageCells returns the text of the token usage table, row by row.
func (d *StatisticsDialog) UsageCells() [][]string {
	out := make([][]string, len(d.usage))
	for i, r := range d.usage {
		out[i] = []string{r.LLM, r.AllTime, r.Last30, r.Last7}
	}
	return out
}

// BannerText returns the error shown in the dialog, or "".
func (d *StatisticsDialog) BannerText() string { return d.banner }

// ResetMistakesClick presses Reset statistics.
func (d *StatisticsDialog) ResetMistakesClick() {
	d.confirm("Delete every recorded mistake? The counts start again from zero. This cannot be undone.", func(yes bool) {
		if !yes {
			return
		}
		if err := d.backend.ResetMistakes(d.ctx); err != nil {
			d.fail(err)
			return
		}
		d.clearBanner()
		d.reload()
		call(d.hooks.Reset)
	})
}

// ResetUsageClick presses Reset token usage.
func (d *StatisticsDialog) ResetUsageClick() {
	d.confirm("Delete the token usage records? This cannot be undone.", func(yes bool) {
		if !yes {
			return
		}
		if err := d.backend.ResetUsage(d.ctx); err != nil {
			d.fail(err)
			return
		}
		d.clearBanner()
		d.loadUsage()
	})
}

func (d *StatisticsDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner = userMessage(err)
	d.refresh()
}

func (d *StatisticsDialog) clearBanner() { d.banner = ""; d.refresh() }

// ---- Drawing ----

// Build draws the dialog.
func (d *StatisticsDialog) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return d.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		tabs := m.NewRow([]widgets.Widget{
			tabButton("Mistakes", d.tab == 0, func() { d.SelectTab(0) }), spaceX(8),
			tabButton("Token usage", d.tab == 1, func() { d.SelectTab(1) }),
		})
		content := d.mistakesTab(bc)
		if d.tab == 1 {
			content = d.usageTab(bc)
		}
		body := []widgets.Widget{tabs, m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(8.0)}), m.NewExpanded(content)}
		if d.banner != "" {
			body = append(body, errorLine(bc, d.banner))
		}
		return frame(bc, "Statistics", 720, 560, m.NewColumn(body, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}),
			primary("Close", d.Close))
	})
}

func tabButton(label string, on bool, f func()) widgets.Widget {
	if on {
		return m.NewFilledButtonTonal(f, m.NewText(label))
	}
	return m.NewTextButton(f, m.NewText(label))
}

// choiceRow is a label and a row of choices, one of which is selected.
func choiceRow(label string, names []string, selected int, pick func(int)) widgets.Widget {
	items := []widgets.Widget{m.NewText(label), spaceX(6)}
	for i, n := range names {
		items = append(items, tabButton(n, i == selected, func() { pick(i) }), spaceX(4))
	}
	return m.NewWrap(items, m.WrapOpts{CrossAxisAlignment: m.WrapCrossAlignmentCenter})
}

func (d *StatisticsDialog) mistakesTab(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	cat, per := 0, 0
	for i, c := range statsCategoryChoices {
		if c.value == d.filter.Category {
			cat = i
		}
	}
	for i, p := range statsPeriodChoices {
		if p.period == d.filter.Period {
			per = i
		}
	}
	catNames, perNames := make([]string, len(statsCategoryChoices)), make([]string, len(statsPeriodChoices))
	for i, c := range statsCategoryChoices {
		catNames[i] = c.label
	}
	for i, p := range statsPeriodChoices {
		perNames[i] = p.label
	}
	var ex []widgets.Widget
	for _, e := range d.examples {
		ex = append(ex, m.NewText(e))
	}
	return m.NewColumn([]widgets.Widget{
		choiceRow("Category:", catNames, cat, d.SetCategory),
		choiceRow("Period:", perNames, per, d.SetPeriod),
		m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 0), m.NewText(d.Summary(), m.TextOpts{Style: theme.TextTheme.TitleSmall})),
		m.NewExpanded(d.table(bc)),
		m.NewText("What you wrote (the three most recent):"),
		m.NewSizedBox(m.NewColumn(ex, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart}), m.SizedBoxOpts{Height: foundation.Some(70.0)}),
		m.NewRow([]widgets.Widget{tipped("Delete every recorded mistake", button("Reset statistics", d.ResetMistakesClick))}),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

// table draws the mistakes table: a header that sorts and rows that select.
func (d *StatisticsDialog) table(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	flex := func(c view.StatsColumn) int {
		if c == view.ColType {
			return 3
		}
		return 2
	}
	header := make([]widgets.Widget, view.StatsColumnCount)
	for c := view.StatsColumn(0); c < view.StatsColumnCount; c++ {
		title := view.StatsColumnTitles[c]
		if d.sort.Column == c {
			title += " ▲"
			if d.sort.Descending {
				title = view.StatsColumnTitles[c] + " ▼"
			}
		}
		header[c] = m.NewExpanded(m.NewGestureDetector(
			m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 6), m.NewText(title, m.TextOpts{Style: theme.TextTheme.LabelLarge})),
			m.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { d.ClickHeader(int(c)) }}),
			m.ExpandedOpts{Flex: foundation.Some(flex(c))})
	}
	rows := make([]widgets.Widget, len(d.shown))
	for i, r := range d.shown {
		cells := view.StatsCells(r)
		cs := make([]widgets.Widget, view.StatsColumnCount)
		for c := view.StatsColumn(0); c < view.StatsColumnCount; c++ {
			align := ui.TextAlignStart
			if c >= view.ColTotal && c <= view.ColLast30 {
				align = ui.TextAlignEnd
			}
			cs[c] = m.NewExpanded(m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 6), m.NewText(cells[c], m.TextOpts{TextAlign: align})),
				m.ExpandedOpts{Flex: foundation.Some(flex(c))})
		}
		var bg ui.Color
		if i == d.selected {
			bg = theme.ColorScheme.PrimaryContainer()
		}
		rows[i] = m.NewListener(m.NewColoredBox(bg, m.NewRow(cs)),
			widgets.ListenerOpts{OnPointerDown: func(*gestures.PointerDownEvent) { d.SelectRow(i) }})
	}
	return m.NewColumn([]widgets.Widget{m.NewRow(header), m.NewDivider(),
		m.NewExpanded(m.NewListView(rows))}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

func (d *StatisticsDialog) usageTab(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	line := func(cells []string, style *painting.TextStyle) widgets.Widget {
		cs := make([]widgets.Widget, len(cells))
		for i, c := range cells {
			cs[i] = m.NewExpanded(m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 6), m.NewText(c, m.TextOpts{Style: style})))
		}
		return m.NewRow(cs)
	}
	rows := []widgets.Widget{line([]string{"LLM", "All time", "Last 30 days", "Last 7 days"}, theme.TextTheme.LabelLarge), m.NewDivider()}
	for _, c := range d.UsageCells() {
		rows = append(rows, line(c, nil))
	}
	return m.NewColumn([]widgets.Widget{
		m.NewText("Tokens sent and received per LLM. A ~ marks counts that were estimated because the provider reported none."),
		m.NewExpanded(m.NewListView(rows)),
		m.NewRow([]widgets.Widget{button("Reset token usage", d.ResetUsageClick)}),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}
