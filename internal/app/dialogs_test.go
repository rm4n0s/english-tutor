package app

import (
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// closeModalSoon closes the next modal dialog that opens, the way a user who
// presses Escape would. The dialogs under test are modal and block in their own
// event loop, so something must end them from inside it.
func closeModalSoon() (stop func()) {
	timer := qt.NewQTimer()
	timer.OnTimeout(func() {
		if w := qt.QApplication_ActiveModalWidget(); w != nil {
			timer.Stop()
			w.Close()
		}
	})
	timer.Start(5)
	return func() { timer.Stop(); timer.Delete() }
}

func TestQtDialogsCanBeDismissed(t *testing.T) {
	st := newStore(t)
	withApp(t, st, func(a *App, _ *syncBuffer) {
		d := qtDialogs{a: a}
		tests := []struct {
			name string
			run  func() bool // reports whether the dialog said "go ahead"
		}{
			{"open file", func() bool { _, ok := d.ChooseOpenFile(); return ok }},
			{"save file", func() bool { _, ok := d.ChooseSaveFile("essay.txt"); return ok }},
			{"document title", func() bool { _, ok := d.AskDocumentTitle("Title"); return ok }},
			{"library", func() bool { _, ok := d.ChooseDocument(); return ok }},
			{"unsaved changes", func() bool { return d.AskUnsaved("essay.txt") != UnsavedCancel }},
		}
		for _, tt := range tests {
			stop := closeModalSoon()
			if tt.run() {
				t.Errorf("%s: closing the dialog must not count as confirming it", tt.name)
			}
			stop()
		}
	})
}

func TestLibraryBackend(t *testing.T) {
	st := newStore(t)
	b := libraryBackend{store: st}
	ctx := t.Context()
	if docs, err := b.ListDocuments(ctx); err != nil || len(docs) != 0 {
		t.Fatalf("empty library: %v, %v", docs, err)
	}
	if err := b.RenameDocument(ctx, 7, "x"); err == nil {
		t.Error("renaming a missing document must fail")
	}
	if err := b.DeleteDocument(ctx, 7); err == nil {
		t.Error("deleting a missing document must fail")
	}
}

func TestLibraryBackendRenameKeepsTheContent(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	doc := &store.Document{Title: "Old", Content: "the text"}
	if err := st.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}
	b := libraryBackend{store: st}
	if err := b.RenameDocument(ctx, doc.ID, "New"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDocument(ctx, doc.ID)
	if err != nil || got.Title != "New" || got.Content != "the text" {
		t.Errorf("document = %+v, %v", got, err)
	}
	if err := b.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatal(err)
	}
	if docs, _ := b.ListDocuments(ctx); len(docs) != 0 {
		t.Errorf("documents = %+v", docs)
	}
}
