package app

import (
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"time"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/logging"
)

// renderInterval throttles how often a streaming explanation is re-rendered
// (about 10 times a second, section 4.4).
const renderInterval = 100 * time.Millisecond

// refreshList redraws the issues list from the issue state (section 4.3).
func (a *App) refreshList() {
	visible := a.state.Visible()
	rows := make([]issues.Row, len(visible))
	for i, it := range visible {
		count := 0
		if a.resultRecorded {
			count = a.typeCounts[it.Type.ID]
		}
		rows[i] = issues.RowFor(it, count)
	}
	selected := 0
	if sel, ok := a.state.Selected(); ok {
		selected = sel.ID
	}
	a.win.SetIssues(a.state.Header(), rows, selected, a.state.EmptyText())
}

// refreshCounts reads the all-time count of every error type and redraws the
// row badges.
func (a *App) refreshCounts() {
	a.goBackground(func() {
		rows, err := a.stats.TypeStats(a.ctx)
		a.rt.Post(func() {
			if err != nil {
				logging.LogError(a.log, "reading statistics", err)
				return
			}
			counts := make(map[string]int, len(rows))
			for _, r := range rows {
				counts[r.ErrorType] = r.Total
			}
			a.typeCounts = counts
			a.refreshList()
		})
	})
}

// onSelectionChanged starts the explanation of the selected issue, or returns
// the panel to idle when nothing is selected (section 5.4).
func (a *App) onSelectionChanged() {
	sel, ok := a.state.Selected()
	if !ok {
		a.explainSeq++ // a result of the explanation that was running is stale now
		a.explainer.Cancel()
		a.win.SetExplanation(view.ExplainIdle, "", "")
		return
	}
	a.explain(sel, false)
}

func (a *App) onRegenerate() {
	if sel, ok := a.state.Selected(); ok {
		a.explain(sel, true)
	}
}

// explain asks the selected LLM to explain the issue and streams the answer
// into the panel.
func (a *App) explain(it issues.Item, regenerate bool) {
	a.explainSeq++
	seq := a.explainSeq
	a.explainBuf.Reset()
	a.explainDirty = false
	a.renderTimer.Stop()
	a.explainHeader = it.Type.Label
	a.win.SetExplanation(view.ExplainLoading, a.explainHeader, "Explaining **"+it.Text+"**...")
	a.loadHeaderCounts(seq, it)

	m, err := a.currentModel()
	if err != nil {
		a.explainFailed(err)
		return
	}
	text := a.win.Text()
	um := analysis.NewUTF16Map(text)
	req := analysis.ExplainRequest{
		Key:        fmt.Sprintf("%d:%d", a.state.CheckID(), it.ID),
		Regenerate: regenerate,
		Text:       text,
		Start:      um.ByteOffset(it.Start),
		End:        um.ByteOffset(it.End),
		Issue:      it.Issue,
	}
	a.goBackground(func() {
		res, err := a.explainer.Explain(a.ctx, m, req, func(chunk string) {
			a.rt.Post(func() { a.onExplainChunk(seq, chunk) })
		})
		a.rt.Post(func() { a.finishExplain(seq, res, err) })
	})
}

// loadHeaderCounts fills the numbers into the header line: how often the user
// has made this kind of mistake.
func (a *App) loadHeaderCounts(seq uint64, it issues.Item) {
	counted := a.resultRecorded
	a.goBackground(func() {
		c, err := a.stats.CountForType(a.ctx, it.Type.ID)
		a.rt.Post(func() {
			if seq != a.explainSeq {
				return
			}
			if err != nil {
				logging.LogError(a.log, "reading statistics", err)
				return
			}
			a.explainHeader = view.FormatExplainHeader(it.Type.Label, c.Total, c.Last7, counted)
			a.win.SetExplanationHeader(a.explainHeader)
		})
	})
}

func (a *App) onExplainChunk(seq uint64, chunk string) {
	if seq != a.explainSeq {
		return
	}
	a.explainBuf.WriteString(chunk)
	a.explainDirty = true
	if !a.renderTimer.IsActive() {
		a.renderNow()
	}
}

func (a *App) renderNow() {
	a.win.SetExplanationMarkdown(a.explainBuf.String())
	a.explainDirty = false
	a.renderTimer.Start(renderInterval)
}

func (a *App) onRenderTimer() {
	if a.explainDirty {
		a.renderNow()
	}
}

func (a *App) finishExplain(seq uint64, res analysis.ExplainResult, err error) {
	if seq != a.explainSeq {
		return
	}
	a.renderTimer.Stop()
	switch {
	case err != nil:
		a.explainFailed(err)
	case res.Cancelled:
		a.win.SetExplanation(view.ExplainCancelled, a.explainHeader, "")
	default:
		a.addUsage(res.Usage)
		a.win.SetExplanation(view.ExplainReady, a.explainHeader, res.Markdown)
	}
}

func (a *App) explainFailed(err error) {
	logging.LogError(a.log, "explanation failed", err)
	a.win.SetExplanation(view.ExplainFailed, a.explainHeader, userMessage(err))
}

// onIssueActivated is a double click on a row: the keyboard goes to the editor,
// with the cursor at the start of the issue.
func (a *App) onIssueActivated(id int) {
	if it, ok := a.state.Get(id); ok {
		a.win.MoveCursorTo(it.Start)
		a.win.FocusEditor()
	}
}

// onNotAnError removes the selected issue and its count, for when the LLM was
// wrong (section 5.7).
func (a *App) onNotAnError() {
	it, ok := a.state.Selected()
	if !ok {
		return
	}
	a.state.Remove(it.ID)
	key := a.sourceKey
	a.goBackground(func() {
		err := a.stats.NotAnError(a.ctx, key, it.Fingerprint)
		a.rt.Post(func() {
			if err != nil {
				logging.LogError(a.log, "removing a mistake", err)
				a.win.ShowError(userMessage(err))
				return
			}
			a.refreshCounts()
		})
	})
}
