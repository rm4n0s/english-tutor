package ui

import (
	"context"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// StatisticsBackend is what the Statistics dialog reads and resets. *store.Stats
// implements it.
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
	dlg     *qt.QDialog
	ctx     context.Context
	backend StatisticsBackend
	hooks   StatisticsHooks

	rows   []store.TypeStat // every row, unfiltered
	shown  []store.TypeStat // rows after the filter and the sort
	filter StatsFilter
	sort   StatsSort

	category   *qt.QComboBox
	period     *qt.QComboBox
	summary    *qt.QLabel
	table      *qt.QTableWidget
	examples   *qt.QListWidget
	resetBtn   *qt.QPushButton
	usageTable *qt.QTableWidget
	resetUsage *qt.QPushButton
	banner     *qt.QLabel

	confirm func(text string) bool // replaced in tests
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

// NewStatisticsDialog builds the dialog and loads the statistics into it.
func NewStatisticsDialog(ctx context.Context, parent *qt.QWidget, backend StatisticsBackend, hooks StatisticsHooks) *StatisticsDialog {
	d := &StatisticsDialog{ctx: ctx, backend: backend, hooks: hooks, sort: DefaultStatsSort}
	d.dlg = qt.NewQDialog(parent)
	d.dlg.SetWindowTitle("Statistics")
	d.dlg.SetModal(true)
	d.dlg.Resize(720, 520)
	d.confirm = func(text string) bool {
		return qt.QMessageBox_Question2(d.dlg.QWidget, "Statistics", text, qt.QMessageBox__Yes, qt.QMessageBox__No) == int(qt.QMessageBox__Yes)
	}

	layout := qt.NewQVBoxLayout2()
	d.dlg.SetLayout(layout.QLayout)
	tabs := qt.NewQTabWidget2()
	tabs.AddTab(d.buildMistakesTab(), "Mistakes")
	tabs.AddTab(d.buildUsageTab(), "Token usage")
	layout.AddWidget(tabs.QWidget)

	d.banner = qt.NewQLabel3("")
	d.banner.SetWordWrap(true)
	d.banner.SetStyleSheet("color: " + colorError + ";")
	d.banner.Hide()
	layout.AddWidget(d.banner.QWidget)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddStretch()
	closeBtn := qt.NewQPushButton3("Close")
	closeBtn.SetDefault(true)
	closeBtn.OnClicked(d.dlg.Accept)
	buttons.AddWidget(closeBtn.QWidget)
	layout.AddLayout(buttons.QLayout)

	d.reload()
	return d
}

// Exec shows the dialog and returns when it is closed.
func (d *StatisticsDialog) Exec() { d.dlg.Exec() }

// Dialog returns the dialog widget.
func (d *StatisticsDialog) Dialog() *qt.QDialog { return d.dlg }

// SelectRow selects a row of the mistakes table, as a click would, and shows
// the examples of its error type.
func (d *StatisticsDialog) SelectRow(row int) { d.table.SelectRow(row) }

func (d *StatisticsDialog) buildMistakesTab() *qt.QWidget {
	tab := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout2()
	tab.SetLayout(layout.QLayout)

	filters := qt.NewQHBoxLayout2()
	d.category = qt.NewQComboBox2()
	for _, c := range statsCategoryChoices {
		d.category.AddItem(c.label)
	}
	d.period = qt.NewQComboBox2()
	for _, p := range statsPeriodChoices {
		d.period.AddItem(p.label)
	}
	filters.AddWidget(qt.NewQLabel3("Category:").QWidget)
	filters.AddWidget(d.category.QWidget)
	filters.AddWidget(qt.NewQLabel3("Period:").QWidget)
	filters.AddWidget(d.period.QWidget)
	filters.AddStretch()
	layout.AddLayout(filters.QLayout)

	d.summary = qt.NewQLabel3("")
	d.summary.SetStyleSheet("font-weight: bold;")
	layout.AddWidget(d.summary.QWidget)

	d.table = qt.NewQTableWidget(nil)
	d.table.SetColumnCount(int(statsColumnCount))
	d.table.SetHorizontalHeaderLabels(statsColumnTitles[:])
	d.table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	d.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	d.table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	d.table.VerticalHeader().Hide()
	header := d.table.HorizontalHeader()
	header.SetSectionsClickable(true)
	header.SetSortIndicatorShown(true)
	header.SetSectionResizeMode2(int(ColType), qt.QHeaderView__Stretch)
	layout.AddWidget2(d.table.QWidget, 1)

	layout.AddWidget(qt.NewQLabel3("What you wrote (the three most recent):").QWidget)
	d.examples = qt.NewQListWidget2()
	d.examples.SetMaximumHeight(90)
	layout.AddWidget(d.examples.QWidget)

	row := qt.NewQHBoxLayout2()
	d.resetBtn = qt.NewQPushButton3("Reset statistics")
	d.resetBtn.SetToolTip("Delete every recorded mistake")
	row.AddWidget(d.resetBtn.QWidget)
	row.AddStretch()
	layout.AddLayout(row.QLayout)

	d.category.OnCurrentIndexChanged(func(i int) {
		if i >= 0 && i < len(statsCategoryChoices) {
			d.filter.Category = statsCategoryChoices[i].value
			d.redraw()
		}
	})
	d.period.OnCurrentIndexChanged(func(i int) {
		if i >= 0 && i < len(statsPeriodChoices) {
			d.filter.Period = statsPeriodChoices[i].period
			d.redraw()
		}
	})
	header.OnSectionClicked(func(col int) {
		if col >= 0 && col < int(statsColumnCount) {
			d.sort = NextSort(d.sort, StatsColumn(col))
			d.redraw()
		}
	})
	d.table.OnCurrentCellChanged(func(row, _, _, _ int) { d.showExamples(row) })
	d.resetBtn.OnClicked(d.resetMistakes)
	return tab
}

func (d *StatisticsDialog) buildUsageTab() *qt.QWidget {
	tab := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout2()
	tab.SetLayout(layout.QLayout)

	note := qt.NewQLabel3("Tokens sent and received per LLM. A ~ marks counts that were estimated because the provider reported none.")
	note.SetWordWrap(true)
	layout.AddWidget(note.QWidget)

	d.usageTable = qt.NewQTableWidget(nil)
	d.usageTable.SetColumnCount(4)
	d.usageTable.SetHorizontalHeaderLabels([]string{"LLM", "All time", "Last 30 days", "Last 7 days"})
	d.usageTable.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	d.usageTable.SetSelectionMode(qt.QAbstractItemView__NoSelection)
	d.usageTable.VerticalHeader().Hide()
	d.usageTable.HorizontalHeader().SetSectionResizeMode(qt.QHeaderView__Stretch)
	layout.AddWidget2(d.usageTable.QWidget, 1)

	row := qt.NewQHBoxLayout2()
	d.resetUsage = qt.NewQPushButton3("Reset token usage")
	row.AddWidget(d.resetUsage.QWidget)
	row.AddStretch()
	layout.AddLayout(row.QLayout)
	d.resetUsage.OnClicked(d.resetTokenUsage)
	return tab
}

// reload reads everything from the backend and redraws both tables.
func (d *StatisticsDialog) reload() {
	rows, err := d.backend.TypeStats(d.ctx)
	if err != nil {
		d.fail(err)
		rows = nil
	}
	d.rows = rows
	d.redraw()
	d.loadUsage()
}

// redraw applies the filter and the sort to the loaded rows.
func (d *StatisticsDialog) redraw() {
	d.shown = FilterStats(d.rows, d.filter)
	SortStats(d.shown, d.sort)
	d.summary.SetText(StatsSummary(d.rows, d.filter))

	order := qt.AscendingOrder
	if d.sort.Descending {
		order = qt.DescendingOrder
	}
	d.table.HorizontalHeader().SetSortIndicator(int(d.sort.Column), order)

	d.table.ClearContents()
	d.table.SetRowCount(len(d.shown))
	for r, row := range d.shown {
		cells := StatsCells(row)
		for c := StatsColumn(0); c < statsColumnCount; c++ {
			item := qt.NewQTableWidgetItem2(cells[c])
			if c >= ColTotal && c <= ColLast30 {
				item.SetTextAlignment(int(qt.AlignRight | qt.AlignVCenter))
			}
			d.table.SetItem(r, int(c), item)
		}
	}
	d.showExamples(-1)
}

func (d *StatisticsDialog) showExamples(row int) {
	d.examples.Clear()
	if row < 0 || row >= len(d.shown) {
		return
	}
	examples, err := d.backend.RecentExamples(d.ctx, d.shown[row].ErrorType, examplesShown)
	if err != nil {
		d.fail(err)
		return
	}
	for _, e := range examples {
		d.examples.AddItem(FormatExample(e))
	}
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
	rows := UsageRows(lists[0], lists[1], lists[2])
	d.usageTable.ClearContents()
	d.usageTable.SetRowCount(len(rows))
	for r, row := range rows {
		for c, text := range []string{row.LLM, row.AllTime, row.Last30, row.Last7} {
			d.usageTable.SetItem(r, c, qt.NewQTableWidgetItem2(text))
		}
	}
}

func (d *StatisticsDialog) resetMistakes() {
	if !d.confirm("Delete every recorded mistake? The counts start again from zero. This cannot be undone.") {
		return
	}
	if err := d.backend.ResetMistakes(d.ctx); err != nil {
		d.fail(err)
		return
	}
	d.clearBanner()
	d.reload()
	call(d.hooks.Reset)
}

func (d *StatisticsDialog) resetTokenUsage() {
	if !d.confirm("Delete the token usage records? This cannot be undone.") {
		return
	}
	if err := d.backend.ResetUsage(d.ctx); err != nil {
		d.fail(err)
		return
	}
	d.clearBanner()
	d.loadUsage()
}

func (d *StatisticsDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner.SetText(userMessage(err))
	d.banner.Show()
}

func (d *StatisticsDialog) clearBanner() {
	d.banner.SetText("")
	d.banner.Hide()
}
