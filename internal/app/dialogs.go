package app

import (
	"context"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/ui"
)

// UnsavedChoice is the answer to "this text has unsaved changes".
type UnsavedChoice int

// The three answers.
const (
	UnsavedSave UnsavedChoice = iota
	UnsavedDiscard
	UnsavedCancel
)

// Dialogs is everything the saving code asks the user. The Qt implementation
// opens modal dialogs; tests use a scripted one.
type Dialogs interface {
	// ChooseOpenFile asks for a text file to open.
	ChooseOpenFile() (path string, ok bool)
	// ChooseSaveFile asks where to save; suggested is the proposed file name.
	ChooseSaveFile(suggested string) (path string, ok bool)
	// AskUnsaved asks what to do with the unsaved changes of the named text.
	AskUnsaved(name string) UnsavedChoice
	// AskDocumentTitle asks for the title of a new library document.
	AskDocumentTitle(suggested string) (title string, ok bool)
	// ChooseDocument shows the Library and returns the document to open.
	ChooseDocument() (id uint, ok bool)
}

// fileFilter is the filter of the file dialogs.
const fileFilter = "Text files (*.txt *.md);;All files (*)"

// qtDialogs implements Dialogs with Qt's modal dialogs.
type qtDialogs struct{ a *App }

func (d qtDialogs) parent() *qt.QWidget { return d.a.win.Widget() }

func (d qtDialogs) ChooseOpenFile() (string, bool) {
	path := qt.QFileDialog_GetOpenFileName4(d.parent(), "Open text", "", fileFilter)
	return path, path != ""
}

func (d qtDialogs) ChooseSaveFile(suggested string) (string, bool) {
	path := qt.QFileDialog_GetSaveFileName4(d.parent(), "Save text", suggested, fileFilter)
	return path, path != ""
}

func (d qtDialogs) AskUnsaved(name string) UnsavedChoice {
	buttons := qt.QMessageBox__Save | qt.QMessageBox__Discard | qt.QMessageBox__Cancel
	answer := qt.QMessageBox_Warning6(d.parent(), "Unsaved changes",
		"Do you want to save the changes to "+name+"?", buttons, qt.QMessageBox__Save)
	switch answer {
	case qt.QMessageBox__Save:
		return UnsavedSave
	case qt.QMessageBox__Discard:
		return UnsavedDiscard
	default:
		return UnsavedCancel
	}
}

func (d qtDialogs) AskDocumentTitle(suggested string) (string, bool) {
	ok := false
	title := qt.QInputDialog_GetText4(d.parent(), "Save to library", "Title:", qt.QLineEdit__Normal, suggested, &ok)
	return title, ok && title != ""
}

func (d qtDialogs) ChooseDocument() (uint, bool) {
	dlg := ui.NewLibraryDialog(d.a.ctx, d.parent(), libraryBackend{store: d.a.store}, ui.LibraryHooks{
		Renamed: func(id uint, title string) { d.a.onDocumentRenamed(id, title) },
		Deleted: func(id uint) { d.a.onDocumentDeleted(id) },
		Failed:  func(err error) { d.a.logError("library", err) },
	})
	dlg.Exec()
	defer dlg.Dialog().Delete()
	return dlg.Opened()
}

// libraryBackend implements ui.LibraryBackend on top of the store.
type libraryBackend struct{ store *store.Store }

var _ ui.LibraryBackend = libraryBackend{}

func (b libraryBackend) ListDocuments(ctx context.Context) ([]store.Document, error) {
	return b.store.ListDocuments(ctx)
}

func (b libraryBackend) RenameDocument(ctx context.Context, id uint, title string) error {
	doc, err := b.store.GetDocument(ctx, id)
	if err != nil {
		return err
	}
	doc.Title = title
	return b.store.UpdateDocument(ctx, doc)
}

func (b libraryBackend) DeleteDocument(ctx context.Context, id uint) error {
	return b.store.DeleteDocument(ctx, id)
}
