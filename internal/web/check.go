package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// renderInterval throttles how often a streaming explanation is sent to the
// browser (about 10 times a second, section 4.4).
const renderInterval = 100 * time.Millisecond

// model builds the model of the LLM selected on the page.
func (w *workspace) model() (analysis.Model, error) {
	cfg, err := w.srv.deps.Store.GetLLMConfig(w.ctx(), w.llmID)
	if err != nil {
		return analysis.Model{}, err
	}
	m, err := w.srv.deps.NewModel(*cfg)
	if err != nil {
		return analysis.Model{}, err
	}
	return analysis.Model{Name: cfg.Name, LLM: m, Temperature: cfg.Temperature,
		Timeout: cfg.Timeout(), MaxParallel: cfg.MaxParallel}, nil
}

// startCheck sends the text to the selected LLM. A manual check (the Check
// button) also records the mistakes it finds in the statistics; an auto-check
// does not, because half-written text would produce counts for half-written
// sentences (section 5.7). The caller holds mu.
func (w *workspace) startCheck(manual bool) {
	text := w.text
	if strings.TrimSpace(text) == "" {
		if manual {
			w.status = ui.StatusIdle
			w.notice = "Nothing to check yet. Write some text first."
			w.touch(rStatus)
		}
		return
	}
	if w.llmID == 0 {
		return
	}
	m, err := w.model()
	if err != nil {
		w.checkFailed(err)
		return
	}

	rev := w.state.BeginCheck()
	w.checkSeq++
	seq := w.checkSeq
	w.checking = true
	w.status = ui.StatusChecking
	w.notice = ""
	w.banner = ""
	w.touch(rToolbar | rBanner | rStatus)

	started := w.run.spawn(func() {
		res, err := w.analyzer.Check(w.ctx(), m, text)
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed {
			return
		}
		w.finishCheck(seq, rev, manual, m.Name, text, res, err)
	})
	if !started { // the server is stopping
		w.checking = false
		w.state.AbortCheck()
	}
}

// finishCheck runs when a check has ended. The caller holds mu.
func (w *workspace) finishCheck(seq, rev uint64, manual bool, llmName, text string, res analysis.CheckResult, err error) {
	w.addUsage(res.Usage) // the tokens were spent whatever happened next
	if seq != w.checkSeq {
		return // a newer check has started; it owns the page now
	}
	w.checking = false
	w.touch(rToolbar | rStatus)

	switch {
	case err != nil:
		w.state.AbortCheck()
		w.checkFailed(err)
		return
	case res.Cancelled:
		w.state.AbortCheck()
		w.status = ui.StatusCancelled
	default:
		out := w.state.FinishCheck(rev, issues.FromPlaced(text, res.Placed))
		w.unplaced = res.Unplaced
		w.resultRecorded = manual
		w.status = ui.StatusIdle
		if out.Discarded {
			w.status = ui.StatusDiscarded
		}
		if manual {
			w.recordMistakes(llmName, text, res.Placed)
		}
	}
}

func (w *workspace) checkFailed(err error) {
	w.logError("check failed", err)
	w.status = ui.StatusFailed
	w.banner = userMessage(err)
	w.touch(rBanner | rStatus | rToolbar)
}

// recordMistakes saves the placed issues of a manual check in the statistics.
// The underlines are already shown, so a failure only adds a banner.
func (w *workspace) recordMistakes(llmName, text string, placed []analysis.Placed) {
	if len(placed) == 0 {
		return
	}
	mistakes := make([]store.Mistake, len(placed))
	for i, p := range placed {
		mistakes[i] = store.Mistake{
			ErrorType:   p.Issue.Type.ID,
			Category:    string(p.Issue.Type.Category),
			Snippet:     text[p.Start:p.End],
			Fingerprint: analysis.Fingerprint(text, p.Start, p.End),
		}
	}
	key := w.sourceKey
	w.run.spawn(func() {
		res, err := w.srv.deps.Stats.Record(w.ctx(), key, llmName, mistakes)
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		if err != nil {
			w.showError("recording mistakes", err)
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		w.srv.deps.Log.Info("mistakes recorded", "inserted", res.Inserted, "duplicates", res.Duplicates, "ignored", res.Ignored)
		w.refreshCountsLater()
	})
}

// addUsage adds a request's tokens to the status bar.
func (w *workspace) addUsage(u llm.Usage) {
	if u.Total() == 0 {
		return
	}
	w.lastUsage = u
	w.sessionUsage = w.sessionUsage.Add(u)
	w.touch(rStatus)
}

// cancelCheck is the Cancel button.
func (w *workspace) cancelCheck() { w.analyzer.Cancel() }

// onStateChange redraws what depends on the issue state. It runs inside the
// state's methods, so mu is held.
func (w *workspace) onStateChange(c issues.Change) {
	switch c.Kind {
	case issues.IssuesChanged, issues.SelectionChanged, issues.VisibilityChanged:
		w.touch(rUnder | rToolbar)
	}
	switch c.Kind {
	case issues.SelectionChanged:
		if sel, ok := w.state.Selected(); ok && !w.fromEditor {
			w.gotoSeq++
			w.gotoPos = sel.Start
			w.touch(rSignals)
		}
		w.onSelectionChanged()
	case issues.IssuesChanged:
		if w.lastCheckID != w.state.CheckID() {
			// A new result: the keys of the cached explanations mean other
			// issues now.
			w.lastCheckID = w.state.CheckID()
			w.explainer.Reset()
		}
		if !w.checking {
			w.touch(rStatus)
		}
	}
	switch c.Kind {
	case issues.IssuesChanged, issues.SelectionChanged, issues.VisibilityChanged, issues.PhaseChanged:
		w.touch(rIssues)
	}
}

// onSelectionChanged starts the explanation of the selected issue, or returns
// the panel to idle when nothing is selected (section 5.4).
func (w *workspace) onSelectionChanged() {
	sel, ok := w.state.Selected()
	if !ok {
		w.explainSeq++ // a result of the explanation that was running is stale now
		w.explainer.Cancel()
		w.explainState = ui.ExplainIdle
		w.explainBody, w.explainHeader = "", ""
		w.touch(rExplain)
		return
	}
	w.explain(sel, false)
}

// explain asks the selected LLM to explain the issue and streams the answer to
// the page. The caller holds mu.
func (w *workspace) explain(it issues.Item, regenerate bool) {
	w.explainSeq++
	seq := w.explainSeq
	w.explainBuf.Reset()
	w.explainHeader = it.Type.Label
	w.explainTitle = it.Text
	w.explainState = ui.ExplainLoading
	w.explainBody = ""
	w.touch(rExplain)
	w.loadHeaderCounts(seq, it)

	m, err := w.model()
	if err != nil {
		w.explainFailed(err)
		return
	}
	text := w.text
	um := analysis.NewUTF16Map(text)
	req := analysis.ExplainRequest{
		Key:        fmt.Sprintf("%d:%d", w.state.CheckID(), it.ID),
		Regenerate: regenerate,
		Text:       text,
		Start:      um.ByteOffset(it.Start),
		End:        um.ByteOffset(it.End),
		Issue:      it.Issue,
	}
	started := w.run.spawn(func() {
		res, err := w.explainer.Explain(w.ctx(), m, req, func(chunk string) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if seq != w.explainSeq || w.closed {
				return
			}
			w.explainBuf.WriteString(chunk)
			w.explainBody = w.explainBuf.String()
			w.touch(rExplain)
		})
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed {
			return
		}
		w.finishExplain(seq, res, err)
	})
	if !started {
		w.explainState = ui.ExplainIdle
	}
}

// loadHeaderCounts fills the numbers into the header line: how often the user
// has made this kind of mistake.
func (w *workspace) loadHeaderCounts(seq uint64, it issues.Item) {
	counted := w.resultRecorded
	w.run.spawn(func() {
		c, err := w.srv.deps.Stats.CountForType(w.ctx(), it.Type.ID)
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed || seq != w.explainSeq {
			return
		}
		if err != nil {
			w.logError("reading statistics", err)
			return
		}
		w.explainHeader = ui.FormatExplainHeader(it.Type.Label, c.Total, c.Last7, counted)
		w.touch(rExplain)
	})
}

func (w *workspace) finishExplain(seq uint64, res analysis.ExplainResult, err error) {
	if seq != w.explainSeq {
		return
	}
	switch {
	case err != nil:
		w.explainFailed(err)
	case res.Cancelled:
		w.explainState = ui.ExplainCancelled
		w.explainBody = ""
		w.touch(rExplain)
	default:
		w.addUsage(res.Usage)
		w.explainState = ui.ExplainReady
		w.explainBody = res.Markdown
		w.touch(rExplain)
	}
}

func (w *workspace) explainFailed(err error) {
	w.logError("explanation failed", err)
	w.explainState = ui.ExplainFailed
	w.explainBody = userMessage(err)
	w.touch(rExplain)
}

// regenerate asks the LLM again for the selected issue.
func (w *workspace) regenerate() {
	if sel, ok := w.state.Selected(); ok {
		w.explain(sel, true)
	}
}

// cancelExplain is the Cancel button of the explanation panel.
func (w *workspace) cancelExplain() { w.explainer.Cancel() }

// notAnError removes the selected issue and its count, for when the LLM was
// wrong (section 5.7).
func (w *workspace) notAnError() {
	it, ok := w.state.Selected()
	if !ok {
		return
	}
	w.state.Remove(it.ID)
	key := w.sourceKey
	w.run.spawn(func() {
		err := w.srv.deps.Stats.NotAnError(w.ctx(), key, it.Fingerprint)
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		if err != nil {
			w.showError("removing a mistake", err)
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		w.refreshCountsLater()
	})
}

// cursorAt selects the issue under the text cursor, or clears the selection
// when the cursor is elsewhere (section 4.2). The cursor itself stays where it is.
func (w *workspace) cursorAt(pos int) {
	w.fromEditor = true
	defer func() { w.fromEditor = false }()
	w.state.SelectAt(pos)
}

// selectIssue is a click on a row of the list: the page moves the cursor to the
// issue and shows its explanation.
func (w *workspace) selectIssue(id int) {
	before, had := w.state.Selected()
	w.state.Select(id)
	// Selecting the issue that is already selected changes nothing and tells
	// nobody, but the click still means "take me there".
	if sel, ok := w.state.Selected(); ok && sel.ID == id && had && before.ID == id {
		w.gotoSeq++
		w.gotoPos = sel.Start
		w.touch(rSignals)
	}
}

// stepIssue is F7 / Shift+F7: select the next or previous issue and put the
// cursor at its start.
func (w *workspace) stepIssue(forward bool, cursor int) {
	step := w.state.Prev
	if forward {
		step = w.state.Next
	}
	before, had := w.state.Selected()
	if it, ok := step(cursor); ok && had && before.ID == it.ID {
		// The only issue stays selected, which tells nobody; the key still
		// means "take me there".
		w.gotoSeq++
		w.gotoPos = it.Start
		w.touch(rSignals)
	}
}

// --- auto-check ---

// restartAutoCheck restarts the pause timer after an edit while auto-check is on.
func (w *workspace) restartAutoCheck() {
	if !w.autoCheck || w.closed {
		return
	}
	if w.autoTimer != nil {
		w.autoTimer.Stop()
	}
	w.autoTimer = time.AfterFunc(w.autoDelay, w.onAutoCheckTimer)
}

func (w *workspace) stopAutoCheck() {
	if w.autoTimer != nil {
		w.autoTimer.Stop()
	}
}

// onAutoCheckTimer runs an auto-check once the user has paused typing. If a
// check is still running it waits for another pause rather than cancelling it.
func (w *workspace) onAutoCheckTimer() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || !w.autoCheck {
		return
	}
	if w.checking {
		w.autoTimer = time.AfterFunc(w.autoDelay, w.onAutoCheckTimer)
		return
	}
	w.startCheck(false)
}

func (w *workspace) setAutoCheck(on bool) {
	w.autoCheck = on
	if !on {
		w.stopAutoCheck()
	}
	w.touch(rToolbar)
}

// selectLLM is a choice in the LLM selector.
func (w *workspace) selectLLM(id uint) {
	for _, c := range w.llms {
		if c.ID == id {
			w.llmID = id
			w.touch(rToolbar)
			return
		}
	}
}

// toggleCategory shows or hides the underlines of a category.
func (w *workspace) toggleCategory(c analysis.Category) {
	w.state.SetCategoryVisible(c, !w.state.CategoryVisible(c))
}
