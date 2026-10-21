package ui

import (
	"context"
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// LibraryBackend is what the Library dialog needs from the rest of the
// application (section 7). internal/app implements it.
type LibraryBackend interface {
	// ListDocuments returns the saved documents, most recently updated first.
	ListDocuments(ctx context.Context) ([]store.Document, error)
	RenameDocument(ctx context.Context, id uint, title string) error
	DeleteDocument(ctx context.Context, id uint) error
}

// LibraryHooks lets the application react to changes made in the dialog.
type LibraryHooks struct {
	// Renamed and Deleted are called after the change was stored.
	Renamed func(id uint, title string)
	Deleted func(id uint)
	// Failed is called with every error the dialog shows, so it can be logged.
	Failed func(err error)
}

// LibraryDialog lists the documents saved in the library, with Open, Rename and
// Delete.
type LibraryDialog struct {
	dlg     *qt.QDialog
	ctx     context.Context
	backend LibraryBackend
	hooks   LibraryHooks

	docs      []store.Document
	list      *qt.QListWidget
	openBtn   *qt.QPushButton
	renameBtn *qt.QPushButton
	deleteBtn *qt.QPushButton
	banner    *qt.QLabel

	opened    uint
	hasOpened bool

	// confirm and askTitle replace the modal question boxes in tests.
	confirm  func(text string) bool
	askTitle func(current string) (string, bool)
}

// NewLibraryDialog builds the dialog and loads the document list.
func NewLibraryDialog(ctx context.Context, parent *qt.QWidget, backend LibraryBackend, hooks LibraryHooks) *LibraryDialog {
	d := &LibraryDialog{ctx: ctx, backend: backend, hooks: hooks}
	d.dlg = qt.NewQDialog(parent)
	d.dlg.SetWindowTitle("Library")
	d.dlg.SetModal(true)
	d.dlg.Resize(520, 380)
	d.confirm = func(text string) bool {
		return qt.QMessageBox_Question2(d.dlg.QWidget, "Library", text, qt.QMessageBox__Yes, qt.QMessageBox__No) == int(qt.QMessageBox__Yes)
	}
	d.askTitle = func(current string) (string, bool) {
		ok := false
		title := qt.QInputDialog_GetText4(d.dlg.QWidget, "Rename document", "Title:", qt.QLineEdit__Normal, current, &ok)
		return title, ok
	}

	layout := qt.NewQVBoxLayout2()
	d.dlg.SetLayout(layout.QLayout)
	d.list = qt.NewQListWidget2()
	layout.AddWidget(d.list.QWidget)

	row := qt.NewQHBoxLayout2()
	d.openBtn = qt.NewQPushButton3("Open")
	d.openBtn.SetDefault(true)
	d.renameBtn = qt.NewQPushButton3("Rename...")
	d.deleteBtn = qt.NewQPushButton3("Delete")
	closeBtn := qt.NewQPushButton3("Close")
	row.AddWidget(d.openBtn.QWidget)
	row.AddWidget(d.renameBtn.QWidget)
	row.AddWidget(d.deleteBtn.QWidget)
	row.AddStretch()
	row.AddWidget(closeBtn.QWidget)
	layout.AddLayout(row.QLayout)

	d.banner = qt.NewQLabel3("")
	d.banner.SetWordWrap(true)
	d.banner.SetStyleSheet("color: " + colorError + ";")
	d.banner.Hide()
	layout.AddWidget(d.banner.QWidget)

	d.openBtn.OnClicked(d.open)
	d.renameBtn.OnClicked(d.rename)
	d.deleteBtn.OnClicked(d.delete)
	closeBtn.OnClicked(d.dlg.Reject)
	d.list.OnItemDoubleClicked(func(*qt.QListWidgetItem) { d.open() })
	d.list.OnCurrentRowChanged(func(int) { d.updateButtons() })
	d.reload(0)
	return d
}

// Exec shows the dialog and returns when it is closed.
func (d *LibraryDialog) Exec() { d.dlg.Exec() }

// Dialog returns the dialog widget.
func (d *LibraryDialog) Dialog() *qt.QDialog { return d.dlg }

// Opened returns the document the user chose to open, if any.
func (d *LibraryDialog) Opened() (id uint, ok bool) { return d.opened, d.hasOpened }

func (d *LibraryDialog) reload(selectID uint) {
	docs, err := d.backend.ListDocuments(d.ctx)
	if err != nil {
		d.fail(err)
		docs = nil
	}
	d.docs = docs
	d.list.Clear()
	row := 0
	for i, doc := range docs {
		d.list.AddItem(libraryRowText(doc))
		if doc.ID == selectID {
			row = i
		}
	}
	if len(docs) == 0 {
		d.list.AddItem("The library is empty. Use Save to library to add a text.")
		d.list.Item(0).SetFlags(qt.NoItemFlags)
	} else {
		d.list.SetCurrentRow(row)
	}
	d.updateButtons()
}

// libraryRowText is one row: the title and the day it was last changed.
func libraryRowText(doc store.Document) string {
	return fmt.Sprintf("%s  ·  %s", doc.Title, view.FormatDate(doc.UpdatedAt))
}

func (d *LibraryDialog) current() (store.Document, bool) {
	row := d.list.CurrentRow()
	if row < 0 || row >= len(d.docs) {
		return store.Document{}, false
	}
	return d.docs[row], true
}

func (d *LibraryDialog) updateButtons() {
	_, ok := d.current()
	d.openBtn.SetEnabled(ok)
	d.renameBtn.SetEnabled(ok)
	d.deleteBtn.SetEnabled(ok)
}

func (d *LibraryDialog) open() {
	if doc, ok := d.current(); ok {
		d.opened, d.hasOpened = doc.ID, true
		d.dlg.Accept()
	}
}

func (d *LibraryDialog) rename() {
	doc, ok := d.current()
	if !ok {
		return
	}
	title, ok := d.askTitle(doc.Title)
	if !ok || title == "" || title == doc.Title {
		return
	}
	if err := d.backend.RenameDocument(d.ctx, doc.ID, title); err != nil {
		d.fail(err)
		return
	}
	d.clearBanner()
	d.reload(doc.ID)
	if d.hooks.Renamed != nil {
		d.hooks.Renamed(doc.ID, title)
	}
}

func (d *LibraryDialog) delete() {
	doc, ok := d.current()
	if !ok || !d.confirm(fmt.Sprintf("Delete %q from the library? This cannot be undone.", doc.Title)) {
		return
	}
	if err := d.backend.DeleteDocument(d.ctx, doc.ID); err != nil {
		d.fail(err)
		d.reload(doc.ID) // the list may be stale
		return
	}
	d.clearBanner()
	d.reload(0)
	if d.hooks.Deleted != nil {
		d.hooks.Deleted(doc.ID)
	}
}

func (d *LibraryDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner.SetText(userMessage(err))
	d.banner.Show()
}

func (d *LibraryDialog) clearBanner() {
	d.banner.SetText("")
	d.banner.Hide()
}
