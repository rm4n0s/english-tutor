package app

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// newSessionKey returns the source key of a new, unsaved text:
// "session:<uuid>" (section 5.7).
func newSessionKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read documents that it never returns an error, so this
		// cannot happen; a source key must not be guessable, so do not go on.
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("session:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// startCheck sends the text to the selected LLM. A manual check (the Check
// button) also records the mistakes it finds in the statistics; an auto-check
// does not, because half-written text would produce counts for half-written
// sentences (section 5.7). Only the request runs off the UI thread.
func (a *App) startCheck(manual bool) {
	if strings.TrimSpace(a.win.Text()) == "" {
		if manual {
			a.status = ui.StatusIdle
			a.win.SetStatus("Nothing to check yet. Write some text first.")
		}
		return
	}
	a.beginCheck(manual, nil)
}

// checkScope is the part of the text a check covers: the bytes [start, end).
type checkScope struct{ start, end int }

// onCheckSelection is the Check entry of the editor's right-click menu: it checks
// only the selection, given in UTF-16 units (section 5.1). It is a manual check.
func (a *App) onCheckSelection(start, end int) {
	text := a.win.Text()
	um := analysis.NewUTF16Map(text)
	scope := checkScope{start: um.ByteOffset(start), end: um.ByteOffset(end)}
	if scope.start >= scope.end || strings.TrimSpace(text[scope.start:scope.end]) == "" {
		return
	}
	a.beginCheck(true, &scope)
}

// beginCheck starts a check of the whole text, or of scope when it is set. A
// manual check puts the loading screen over the editor, so that the text stays as
// it is until the result comes; an auto-check does not, because it fires while the
// user is about to go on typing.
func (a *App) beginCheck(manual bool, scope *checkScope) {
	if a.win.SelectedLLMID() == 0 {
		return
	}
	m, err := a.currentModel()
	if err != nil {
		a.checkFailed(err)
		return
	}

	text := a.win.Text()
	sent, offset := text, 0
	if scope != nil {
		sent, offset = text[scope.start:scope.end], scope.start
	}
	rev := a.state.BeginCheck()
	a.checkSeq++
	seq := a.checkSeq
	a.checking = true
	a.status = ui.StatusChecking
	a.win.SetChecking(true)
	a.win.SetEditorBusy(manual)
	a.win.ClearError()
	a.refreshStatus()

	a.goBackground(func() {
		res, err := a.analyzer.Check(a.ctx, m, sent)
		// The locator counts bytes from the start of what was sent.
		var placed analysis.LocateResult
		placed.Append(res.LocateResult, offset)
		res.LocateResult = placed
		mainthread.Start(func() { a.finishCheck(seq, rev, manual, scope, m.Name, text, res, err) })
	})
}

// currentModel builds the model of the LLM selected in the window.
func (a *App) currentModel() (analysis.Model, error) {
	cfg, err := a.store.GetLLMConfig(a.ctx, a.win.SelectedLLMID())
	if err != nil {
		return analysis.Model{}, err
	}
	model, err := a.newModel(*cfg)
	if err != nil {
		return analysis.Model{}, err
	}
	return analysis.Model{Name: cfg.Name, LLM: model, Temperature: cfg.Temperature,
		Timeout: cfg.Timeout(), MaxParallel: cfg.MaxParallel}, nil
}

// finishCheck runs on the UI thread when a check has ended.
//
// scope is the part of the text that was checked, nil for all of it; the offsets of
// res are bytes of text either way.
func (a *App) finishCheck(seq uint64, rev uint64, manual bool, scope *checkScope, llmName, text string, res analysis.CheckResult, err error) {
	a.addUsage(res.Usage) // the tokens were spent whatever happened next
	if seq != a.checkSeq {
		return // a newer check has started; it owns the window now
	}
	a.checking = false
	a.win.SetChecking(false)
	a.win.SetEditorBusy(false)

	switch {
	case err != nil:
		a.state.AbortCheck()
		a.checkFailed(err)
		a.notifyCheckEnd(manual, "Check failed", userMessage(err))
		return
	case res.Cancelled:
		a.state.AbortCheck()
		a.status = ui.StatusCancelled
	default:
		out := a.storeResult(rev, manual, scope, text, res)
		a.status = ui.StatusIdle
		if out.Discarded {
			a.status = ui.StatusDiscarded
		}
		if manual {
			a.recordMistakes(llmName, text, res.Placed)
		}
		if !out.Discarded {
			a.notifyCheckEnd(manual, "Check finished", a.statusText())
		}
	}
	a.refreshStatus()
}

// notifyCheckEnd tells the user that a check they started has ended, when they
// are not looking at the window: a message next to the tray icon and a request
// to the desktop to draw attention to the window. An auto-check is silent, and
// so is a check that ends while the user is in the window and sees it.
func (a *App) notifyCheckEnd(manual bool, title, text string) {
	if !manual || a.windowActive() {
		return
	}
	a.notify(title, text)
	a.win.Alert()
}

// storeResult gives the issues of a finished check to the issue state: all of them
// replace the old ones, or, for a check of part of the text, only those in that
// part do (section 5.1).
func (a *App) storeResult(rev uint64, manual bool, scope *checkScope, text string, res analysis.CheckResult) issues.Outcome {
	items := issues.FromPlaced(text, res.Placed)
	if scope == nil {
		a.unplaced = res.Unplaced
		a.resultRecorded = manual
		return a.state.FinishCheck(rev, items)
	}
	um := analysis.NewUTF16Map(text)
	checked := issues.Range{Start: um.Offset(scope.start), End: um.Offset(scope.end)}
	// The issues outside the selection come from an earlier check; the statistics
	// have them only if that check was manual (and these issues are manual too).
	before := len(a.state.Items())
	recorded := manual && (a.resultRecorded || before == 0)
	out := a.state.FinishRangeCheck(rev, checked, items)
	if !out.Discarded {
		a.unplaced = res.Unplaced
		a.resultRecorded = recorded
	}
	return out
}

func (a *App) checkFailed(err error) {
	logging.LogError(a.log, "check failed", err)
	a.status = ui.StatusFailed
	a.win.ShowError(userMessage(err))
	a.refreshStatus()
}

// userMessage is the text shown for an error: the message of an application
// error is written for the user.
func userMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// recordMistakes saves the placed issues of a manual check in the statistics.
// The underlines are already shown, so a failure only adds a banner.
func (a *App) recordMistakes(llmName, text string, placed []analysis.Placed) {
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
	key := a.sourceKey
	a.goBackground(func() {
		res, err := a.stats.Record(a.ctx, key, llmName, mistakes)
		mainthread.Start(func() {
			if err != nil {
				logging.LogError(a.log, "recording mistakes", err)
				a.win.ShowError(userMessage(err))
				return
			}
			a.log.Info("mistakes recorded", "inserted", res.Inserted, "duplicates", res.Duplicates, "ignored", res.Ignored)
			a.refreshCounts()
		})
	})
}

// addUsage adds a request's tokens to the status bar.
func (a *App) addUsage(u llm.Usage) {
	if u.Total() == 0 {
		return
	}
	a.lastUsage = u
	a.sessionUsage = a.sessionUsage.Add(u)
	a.win.SetTokens(a.lastUsage, a.sessionUsage)
}

func (a *App) refreshStatus() { a.win.SetStatus(a.statusText()) }

// statusText is the left part of the status bar.
func (a *App) statusText() string {
	return ui.FormatStatus(a.status, a.state.Phase() == issues.Checked, len(a.state.Items()), a.unplaced)
}

// onStateChange redraws what depends on the issue state.
func (a *App) onStateChange(c issues.Change) {
	switch c.Kind {
	case issues.IssuesChanged, issues.SelectionChanged, issues.VisibilityChanged:
		a.refreshUnderlines()
	}
	switch c.Kind {
	case issues.SelectionChanged:
		if sel, ok := a.state.Selected(); ok && !a.fromEditor {
			a.win.MoveCursorTo(sel.Start)
		}
		a.onSelectionChanged()
	case issues.IssuesChanged:
		if a.lastCheckID != a.state.CheckID() {
			// A new result: the keys of the cached explanations mean other
			// issues now.
			a.lastCheckID = a.state.CheckID()
			a.explainer.Reset()
		}
		if !a.checking {
			a.refreshStatus()
		}
	}
	switch c.Kind {
	case issues.IssuesChanged, issues.SelectionChanged, issues.VisibilityChanged, issues.PhaseChanged:
		a.refreshList()
	}
}

func (a *App) refreshUnderlines() {
	selected, hasSelected := a.state.Selected()
	visible := a.state.Visible()
	us := make([]ui.Underline, len(visible))
	for i, it := range visible {
		us[i] = ui.Underline{Start: it.Start, End: it.End, Category: it.Category(), Selected: hasSelected && it.ID == selected.ID}
	}
	a.win.SetUnderlines(us)
}

// onCursorMoved selects the issue under the text cursor, or clears the
// selection when the cursor is elsewhere (section 4.2).
func (a *App) onCursorMoved(pos int) {
	a.fromEditor = true
	defer func() { a.fromEditor = false }()
	a.state.SelectAt(pos)
}

// stepIssue is F7 / Shift+F7: select the next or previous issue and put the
// cursor at its start.
func (a *App) stepIssue(step func(cursor int) (issues.Item, bool)) {
	if it, ok := step(a.win.CursorPosition()); ok {
		a.win.MoveCursorTo(it.Start)
	}
}

// tooltipAt is the tooltip of the underline at a text position: the error
// type, its category, and the short message (section 4.2).
func (a *App) tooltipAt(pos int) string {
	for _, it := range a.state.Visible() {
		if pos >= it.Start && pos < it.End {
			return fmt.Sprintf("%s · %s\n%s", it.Type.Label, it.Type.Category.Label(), it.Message)
		}
	}
	return ""
}

// onTextChanged restarts the auto-check timer while auto-check is on.
func (a *App) onTextChanged() {
	if a.autoCheck {
		a.autoTimer.Start(int(a.autoDelay / time.Millisecond))
	}
}

func (a *App) setAutoCheck(on bool) {
	a.autoCheck = on
	if !on {
		a.autoTimer.Stop()
	}
}

// onAutoCheckTimer runs an auto-check once the user has paused typing. If a
// check is still running it waits for another pause rather than cancelling it.
func (a *App) onAutoCheckTimer() {
	if !a.autoCheck {
		return
	}
	if a.checking {
		a.autoTimer.Start(int(a.autoDelay / time.Millisecond))
		return
	}
	a.startCheck(false)
}
