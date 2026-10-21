package app

import (
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/logging"
)

// openStatistics shows the modal Statistics dialog.
func (a *App) openStatistics() {
	a.win.ShowDialog(gui.NewStatisticsDialog(a.ctx, a.win, a.stats, gui.StatisticsHooks{
		Reset:  a.onStatisticsReset,
		Failed: func(err error) { logging.LogError(a.log, "statistics", err) },
	}))
}

// onStatisticsReset forgets the counts shown in the list and the panel header
// after the recorded mistakes were deleted.
func (a *App) onStatisticsReset() {
	a.typeCounts = map[string]int{}
	a.refreshList()
	if sel, ok := a.state.Selected(); ok {
		a.loadHeaderCounts(a.explainSeq, sel)
	}
}
