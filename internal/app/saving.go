package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// appTitle is the name shown in the window title.
const appTitle = "English Tutor"

// untitled is the name of a text that has not been saved anywhere.
const untitled = "Untitled"

// titleLimit is the longest default title of a library document (section 7).
const titleLimit = 60

// windowTitle is the window title: the name of the text and a * when it has
// unsaved changes (section 7).
func windowTitle(name string, modified bool) string {
	if modified {
		name += "*"
	}
	return name + " — " + appTitle
}

// defaultDocumentTitle is the title proposed for a new library document: the
// first line of the text, shortened to 60 characters.
func defaultDocumentTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return issues.ShortenSnippet(line, titleLimit)
		}
	}
	return untitled
}

// place describes where the text came from or was last saved to. The text has
// one identity at a time, like a buffer in VS Code (section 5.7): its source key
// follows the most recent place.
type place struct {
	filePath   string // absolute path of the file the text is tied to, or ""
	lineEnding string // the file's line ending, written back on save
	docID      uint   // library document the text is tied to, or 0
	docTitle   string
	name       string // what the window title shows
}

// displayName is the name of the text for the title and for questions.
func (a *App) displayName() string {
	if a.place.name != "" {
		return a.place.name
	}
	return untitled
}

func (a *App) updateTitle() { a.win.SetTitle(windowTitle(a.displayName(), a.win.Modified())) }

func (a *App) logError(msg string, err error) { logging.LogError(a.log, msg, err) }

// showError tells the user and logs the failure.
func (a *App) showError(msg string, err error) {
	a.logError(msg, err)
	a.win.ShowError(userMessage(err))
}

// resetText replaces the text with one that was just created or loaded: a
// running check or explanation stops, the issues and the cached results of the
// old text go, and the text counts as saved.
func (a *App) resetText(text string) {
	a.analyzer.Cancel()
	a.explainer.Cancel()
	a.checkSeq++ // results of a check that was running are stale now
	a.explainSeq++
	a.checking = false
	a.win.SetChecking(false)
	a.autoTimer.Stop()
	a.status = ui.StatusIdle
	a.unplaced = 0
	a.resultRecorded = false
	a.state.Reset()
	a.win.ClearError()
	a.win.LoadText(text)
	a.autoTimer.Stop() // loading a text is not typing
	a.win.SetExplanation(ui.ExplainIdle, "", "")
	a.refreshStatus()
}

// setSource switches the statistics source of the text to key. With move set,
// the mistakes recorded under the old source follow the text (Save As); without
// it the text simply starts a new source (New, Open).
func (a *App) setSource(key string, move bool) {
	old := a.sourceKey
	a.sourceKey = key
	if move && old != key {
		if err := a.stats.MoveSource(a.ctx, old, key); err != nil {
			a.showError("moving statistics", err)
		}
	}
}

// confirmDiscard asks what to do with unsaved changes before the text is
// replaced or the window closes. It returns true when the caller may go on.
func (a *App) confirmDiscard() bool {
	if !a.win.Modified() {
		return true
	}
	switch a.dialogs.AskUnsaved(a.displayName()) {
	case UnsavedDiscard:
		return true
	case UnsavedSave:
		return a.saveForClose()
	default:
		return false
	}
}

// saveForClose saves in the place the text belongs to: the library if its
// source is a library document, else its file (asking for one if it has none).
func (a *App) saveForClose() bool {
	if strings.HasPrefix(a.sourceKey, "doc:") {
		return a.saveToLibrary()
	}
	return a.save()
}

// onNew starts an empty text.
func (a *App) onNew() {
	if !a.confirmDiscard() {
		return
	}
	a.place = place{lineEnding: lineEndingLF}
	a.resetText("")
	a.setSource(newSessionKey(), false)
	a.updateTitle()
}

// onOpen opens a text file. The file is read before anything is replaced, so a
// file that cannot be opened leaves the current text alone.
func (a *App) onOpen() {
	path, ok := a.dialogs.ChooseOpenFile()
	if !ok {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	text, ending, err := readTextFile(a.ctx, abs)
	if err != nil {
		a.showError("opening a file", err)
		return
	}
	if !a.confirmDiscard() {
		return
	}
	a.place = place{filePath: abs, lineEnding: ending, name: filepath.Base(abs)}
	a.resetText(text)
	a.setSource("file:"+abs, false)
	a.updateTitle()
}

// onSave writes the text to its file, or behaves like Save As if it has none.
func (a *App) onSave() { a.save() }

func (a *App) save() bool {
	if a.place.filePath == "" {
		return a.saveAs()
	}
	if err := writeTextFile(a.ctx, a.place.filePath, a.win.Text(), a.place.lineEnding); err != nil {
		a.showError("saving a file", err)
		return false
	}
	a.win.SetModified(false)
	a.updateTitle()
	return true
}

// onSaveAs asks for a file and saves the text there.
func (a *App) onSaveAs() { a.saveAs() }

func (a *App) saveAs() bool {
	suggested := a.place.filePath
	if suggested == "" {
		suggested = defaultDocumentTitle(a.win.Text()) + ".txt"
	}
	path, ok := a.dialogs.ChooseSaveFile(suggested)
	if !ok {
		return false
	}
	if filepath.Ext(path) == "" {
		path += ".txt"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	ending := a.place.lineEnding
	if ending == "" {
		ending = lineEndingLF
	}
	if err := writeTextFile(a.ctx, abs, a.win.Text(), ending); err != nil {
		a.showError("saving a file", err)
		return false
	}
	a.place.filePath, a.place.lineEnding, a.place.name = abs, ending, filepath.Base(abs)
	a.setSource("file:"+abs, true)
	a.win.SetModified(false)
	a.updateTitle()
	return true
}

// onSaveToLibrary stores the text in the library: a new document (asking for a
// title) or, if the text came from the library, an update of that document.
func (a *App) onSaveToLibrary() { a.saveToLibrary() }

func (a *App) saveToLibrary() bool {
	text := a.win.Text()
	if a.place.docID == 0 {
		title, ok := a.dialogs.AskDocumentTitle(defaultDocumentTitle(text))
		if !ok {
			return false
		}
		doc := &store.Document{Title: title, Content: text}
		if err := a.store.CreateDocument(a.ctx, doc); err != nil {
			a.showError("saving to the library", err)
			return false
		}
		a.place.docID, a.place.docTitle = doc.ID, doc.Title
	} else {
		doc := &store.Document{ID: a.place.docID, Title: a.place.docTitle, Content: text}
		if err := a.store.UpdateDocument(a.ctx, doc); err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("DocumentNotFound") {
				// Deleted from the library in the meantime: the next save
				// creates a new document.
				a.place.docID, a.place.docTitle = 0, ""
			}
			a.showError("saving to the library", err)
			return false
		}
	}
	a.place.name = a.place.docTitle
	a.setSource(fmt.Sprintf("doc:%d", a.place.docID), true)
	a.win.SetModified(false)
	a.updateTitle()
	return true
}

// onLibrary opens the Library and loads the document the user picks.
func (a *App) onLibrary() {
	id, ok := a.dialogs.ChooseDocument()
	if !ok {
		return
	}
	doc, err := a.store.GetDocument(a.ctx, id)
	if err != nil {
		a.showError("opening a library document", err)
		return
	}
	if !a.confirmDiscard() {
		return
	}
	a.place = place{docID: doc.ID, docTitle: doc.Title, name: doc.Title, lineEnding: lineEndingLF}
	a.resetText(doc.Content)
	a.setSource(fmt.Sprintf("doc:%d", doc.ID), false)
	a.updateTitle()
}

// onDocumentRenamed keeps the title of the open document in step with the library.
func (a *App) onDocumentRenamed(id uint, title string) {
	if a.place.docID != id {
		return
	}
	a.place.docTitle = title
	if strings.HasPrefix(a.sourceKey, "doc:") {
		a.place.name = title
		a.updateTitle()
	}
}

// onDocumentDeleted forgets the library document the text was tied to, so that
// the next Save to library creates a new one.
func (a *App) onDocumentDeleted(id uint) {
	if a.place.docID == id {
		a.place.docID, a.place.docTitle = 0, ""
	}
}

// canClose is asked when the user closes the window. With a tray icon the window
// is only hidden, so nothing is lost and nothing is asked; the question comes
// when the application quits (Quit).
func (a *App) canClose() bool { return a.tray != nil || a.confirmDiscard() }
