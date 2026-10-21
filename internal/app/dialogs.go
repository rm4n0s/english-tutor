package app

import (
	"context"

	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// UnsavedChoice is the answer to "this text has unsaved changes".
type UnsavedChoice int

// The three answers.
const (
	UnsavedSave UnsavedChoice = iota
	UnsavedDiscard
	UnsavedCancel
)

// Dialogs is everything the saving code asks the user. The window implements it
// with modal dialogs; tests use a scripted one. glutter has no nested event loop,
// so every question reports its answer to a function instead of returning it. A
// scripted implementation may call the function at once.
type Dialogs interface {
	// ChooseOpenFile asks for a text file to open.
	ChooseOpenFile(done func(path string, ok bool))
	// ChooseSaveFile asks where to save; suggested is the proposed file name.
	ChooseSaveFile(suggested string, done func(path string, ok bool))
	// AskUnsaved asks what to do with the unsaved changes of the named text.
	AskUnsaved(name string, done func(UnsavedChoice))
	// AskDocumentTitle asks for the title of a new library document.
	AskDocumentTitle(suggested string, done func(title string, ok bool))
	// ChooseDocument shows the Library and reports the document to open.
	ChooseDocument(done func(id uint, ok bool))
}

// windowDialogs implements Dialogs with the dialogs of the main window.
type windowDialogs struct{ a *App }

func (d windowDialogs) ChooseOpenFile(done func(string, bool)) { d.a.win.ChooseOpenFile(done) }

func (d windowDialogs) ChooseSaveFile(suggested string, done func(string, bool)) {
	d.a.win.ChooseSaveFile(suggested, done)
}

func (d windowDialogs) AskUnsaved(name string, done func(UnsavedChoice)) {
	d.a.win.Choose("Unsaved changes", "Do you want to save the changes to "+name+"?",
		[]string{"Save", "Discard", "Cancel"}, 2, func(i int) { done(UnsavedChoice(i)) })
}

func (d windowDialogs) AskDocumentTitle(suggested string, done func(string, bool)) {
	d.a.win.AskText("Save to library", "Title", suggested, func(title string, ok bool) { done(title, ok && title != "") })
}

func (d windowDialogs) ChooseDocument(done func(uint, bool)) {
	d.a.win.ShowDialog(gui.NewLibraryDialog(d.a.ctx, d.a.win, libraryBackend{store: d.a.store}, gui.LibraryHooks{
		Renamed: func(id uint, title string) { d.a.onDocumentRenamed(id, title) },
		Deleted: func(id uint) { d.a.onDocumentDeleted(id) },
		Failed:  func(err error) { d.a.logError("library", err) },
		Done:    done,
	}))
}

// libraryBackend implements gui.LibraryBackend on top of the store.
type libraryBackend struct{ store *store.Store }

var _ gui.LibraryBackend = libraryBackend{}

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
