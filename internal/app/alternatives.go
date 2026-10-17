package app

import (
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// onAlternative is the Alternative entry of the editor's right-click menu: it
// opens the Alternatives window for the selection, given in UTF-16 units, and
// asks the selected LLM for other ways to write it (section 5.10). A window that
// is already open is replaced.
func (a *App) onAlternative(start, end int) {
	text := a.win.Text()
	um := analysis.NewUTF16Map(text)
	req := analysis.AlternativesRequest{Text: text, Start: um.ByteOffset(start), End: um.ByteOffset(end)}
	selected := ""
	if req.Start >= 0 && req.Start <= req.End && req.End <= len(text) {
		selected = text[req.Start:req.End]
	}

	a.closeAlternatives()
	var dlg *ui.AlternativesDialog
	dlg = ui.NewAlternativesDialog(a.win.Widget(), selected, ui.AlternativesHooks{
		Retry:  func() { a.requestAlternatives(dlg, req) },
		Chosen: func(alt string) { a.useAlternative(dlg, start, end, selected, alt) },
		Closed: func() { a.dropAlternatives(dlg) },
	})
	a.altDlg = dlg
	dlg.Show()
	a.requestAlternatives(dlg, req)
}

// useAlternative puts the alternative the user double-clicked in the editor in
// place of the selection it was asked for, and closes the window. If the text
// at that place is no longer the selection (the user went on typing), nothing is
// changed and the window says so, because replacing other words would lose text.
func (a *App) useAlternative(dlg *ui.AlternativesDialog, start, end int, selected, alt string) {
	text := a.win.Text()
	um := analysis.NewUTF16Map(text)
	bs, be := um.ByteOffset(start), um.ByteOffset(end)
	if um.Offset(be) != end || text[bs:be] != selected {
		dlg.SetNotice("The text has changed since you selected it, so nothing was replaced. Copy the alternative and paste it instead.")
		return
	}
	a.win.ReplaceRange(start, end, alt)
	a.closeAlternatives()
	a.win.FocusEditor()
}

// requestAlternatives runs the request in the background and shows its outcome
// in dlg.
func (a *App) requestAlternatives(dlg *ui.AlternativesDialog, req analysis.AlternativesRequest) {
	a.altSeq++
	seq := a.altSeq
	dlg.SetLoading()
	m, err := a.currentModel()
	if err != nil {
		a.alternativesFailed(dlg, err)
		return
	}
	a.goBackground(func() {
		res, err := a.suggester.Suggest(a.ctx, m, req)
		mainthread.Start(func() { a.finishAlternatives(seq, dlg, res, err) })
	})
}

// finishAlternatives runs on the UI thread when a request has ended.
func (a *App) finishAlternatives(seq uint64, dlg *ui.AlternativesDialog, res analysis.AlternativesResult, err error) {
	a.addUsage(res.Usage) // the tokens were spent whatever happened next
	if seq != a.altSeq {
		return // the window was closed or asked again; this answer is stale
	}
	switch {
	case err != nil:
		a.alternativesFailed(dlg, err)
	case res.Cancelled:
		// Only a close, a newer request or quitting cancels, and each of them
		// has changed altSeq or ends the application.
	default:
		dlg.SetAlternatives(res.Alternatives)
	}
}

func (a *App) alternativesFailed(dlg *ui.AlternativesDialog, err error) {
	logging.LogError(a.log, "alternatives failed", err)
	dlg.SetFailed(userMessage(err))
}

// closeAlternatives closes the Alternatives window, if one is open.
func (a *App) closeAlternatives() {
	if dlg := a.altDlg; dlg != nil {
		dlg.Close()
		a.dropAlternatives(dlg)
	}
}

// dropAlternatives forgets dlg after it was closed and stops its request. It is
// safe to call more than once.
func (a *App) dropAlternatives(dlg *ui.AlternativesDialog) {
	if a.altDlg != dlg {
		return
	}
	a.altDlg = nil
	a.altSeq++ // an answer that is still on its way is stale now
	a.suggester.Cancel()
	dlg.Dialog().DeleteLater()
}
