package gui

import (
	"context"
	"fmt"

	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
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
	// Done is called once, when the dialog is gone: with the document the user
	// chose to open, or ok false when it was only closed.
	Done func(id uint, ok bool)
}

// LibraryDialog lists the documents saved in the library, with Open, Rename and
// Delete.
type LibraryDialog struct {
	dialogBase
	win     *MainWindow
	ctx     context.Context
	backend LibraryBackend
	hooks   LibraryHooks

	docs   []store.Document
	row    int // the selected row, -1 for none
	banner string

	opened    uint
	hasOpened bool

	// confirm and askTitle ask the questions of Delete and Rename; tests replace them.
	confirm  func(text string, done func(yes bool))
	askTitle func(current string, done func(title string, ok bool))
}

// NewLibraryDialog builds the dialog and loads the document list. Show it with
// win.ShowDialog.
func NewLibraryDialog(ctx context.Context, win *MainWindow, backend LibraryBackend, hooks LibraryHooks) *LibraryDialog {
	d := &LibraryDialog{dialogBase: newDialogBase(), win: win, ctx: ctx, backend: backend, hooks: hooks, row: -1}
	d.confirm = func(text string, done func(bool)) { win.Confirm("Library", text, done) }
	d.askTitle = func(current string, done func(string, bool)) { win.AskText("Rename document", "Title", current, done) }
	d.reload(0)
	return d
}

// Opened returns the document the user chose to open, if any.
func (d *LibraryDialog) Opened() (id uint, ok bool) { return d.opened, d.hasOpened }

func (d *LibraryDialog) reload(selectID uint) {
	docs, err := d.backend.ListDocuments(d.ctx)
	if err != nil {
		d.fail(err)
		docs = nil
	}
	d.docs = docs
	d.row = -1
	if len(docs) > 0 {
		d.row = 0
	}
	for i, doc := range docs {
		if doc.ID == selectID {
			d.row = i
		}
	}
	d.refresh()
}

// libraryRowText is one row: the title and the day it was last changed.
func libraryRowText(doc store.Document) string {
	return fmt.Sprintf("%s  ·  %s", doc.Title, view.FormatDate(doc.UpdatedAt))
}

const libraryEmptyText = "The library is empty. Use Save to library to add a text."

func (d *LibraryDialog) current() (store.Document, bool) {
	if d.row < 0 || d.row >= len(d.docs) {
		return store.Document{}, false
	}
	return d.docs[d.row], true
}

// Build draws the dialog.
func (d *LibraryDialog) Build(bc widgets.BuildContext, close func()) widgets.Widget {
	return d.attach(close, func(bc widgets.BuildContext) widgets.Widget {
		var list widgets.Widget
		if len(d.docs) == 0 {
			list = m.NewText(libraryEmptyText)
		} else {
			list = m.NewListViewBuilder(func(bc widgets.BuildContext, i int) widgets.Widget {
				return listRow(bc, libraryRowText(d.docs[i]), i == d.row,
					func() { d.SelectRow(i) }, func() { d.SelectRow(i); d.Open() })
			}, m.ListViewOpts{ItemCount: m.Some(len(d.docs))})
		}
		_, has := d.current()
		var onOpen, onRename, onDelete func()
		if has {
			onOpen, onRename, onDelete = d.Open, d.Rename, d.Delete
		}
		body := []widgets.Widget{m.NewExpanded(list)}
		if d.banner != "" {
			body = append(body, errorLine(bc, d.banner))
		}
		return frame(bc, "Library", 520, 380, m.NewColumn(body, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}),
			primary("Open", onOpen), spaceX(8), button("Rename...", onRename), spaceX(8), button("Delete", onDelete),
			m.NewExpanded(m.NewSizedBox(nil)), button("Close", d.Close))
	})
}

// Dismissed reports the result.
func (d *LibraryDialog) Dismissed() {
	d.open = false
	if d.hooks.Done != nil {
		d.hooks.Done(d.opened, d.hasOpened)
	}
}

// SelectRow selects a document.
func (d *LibraryDialog) SelectRow(row int) {
	if row >= 0 && row < len(d.docs) {
		d.row = row
		d.refresh()
	}
}

// Rows returns the text of every row, or the empty line.
func (d *LibraryDialog) Rows() []string {
	if len(d.docs) == 0 {
		return []string{libraryEmptyText}
	}
	out := make([]string, len(d.docs))
	for i, doc := range d.docs {
		out[i] = libraryRowText(doc)
	}
	return out
}

// SelectedRow returns the selected row, or -1.
func (d *LibraryDialog) SelectedRow() int { return d.row }

// BannerText returns the error shown in the dialog, or "".
func (d *LibraryDialog) BannerText() string { return d.banner }

// ButtonsEnabled reports whether Open, Rename and Delete can be pressed.
func (d *LibraryDialog) ButtonsEnabled() bool { _, ok := d.current(); return ok }

// Open chooses the selected document and closes the dialog.
func (d *LibraryDialog) Open() {
	if doc, ok := d.current(); ok {
		d.opened, d.hasOpened = doc.ID, true
		d.Close()
	}
}

// Rename asks for a new title for the selected document.
func (d *LibraryDialog) Rename() {
	doc, ok := d.current()
	if !ok {
		return
	}
	d.askTitle(doc.Title, func(title string, ok bool) {
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
	})
}

// Delete asks, and then deletes the selected document.
func (d *LibraryDialog) Delete() {
	doc, ok := d.current()
	if !ok {
		return
	}
	d.confirm(fmt.Sprintf("Delete %q from the library? This cannot be undone.", doc.Title), func(yes bool) {
		if !yes {
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
	})
}

func (d *LibraryDialog) fail(err error) {
	if d.hooks.Failed != nil {
		d.hooks.Failed(err)
	}
	d.banner = userMessage(err)
	d.refresh()
}

func (d *LibraryDialog) clearBanner() { d.banner = ""; d.refresh() }
