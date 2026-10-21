package ui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/uitest"
)

type fakeLibrary struct {
	docs                          []store.Document
	renamed                       map[uint]string
	deleted                       []uint
	listErr, renameErr, deleteErr error
}

func (f *fakeLibrary) ListDocuments(context.Context) ([]store.Document, error) {
	return slices.Clone(f.docs), f.listErr
}

func (f *fakeLibrary) RenameDocument(_ context.Context, id uint, title string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	if f.renamed == nil {
		f.renamed = map[uint]string{}
	}
	f.renamed[id] = title
	for i := range f.docs {
		if f.docs[i].ID == id {
			f.docs[i].Title = title
		}
	}
	return nil
}

func (f *fakeLibrary) DeleteDocument(_ context.Context, id uint) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	f.docs = slices.DeleteFunc(f.docs, func(d store.Document) bool { return d.ID == id })
	return nil
}

func doc(id uint, title string, updated time.Time) store.Document {
	return store.Document{ID: id, Title: title, Content: "text of " + title, UpdatedAt: updated}
}

func newFakeLibrary() *fakeLibrary {
	return &fakeLibrary{docs: []store.Document{doc(3, "Essay on cats", day(15)), doc(2, "Letter", day(10)), doc(1, "Notes", day(2))}}
}

type libraryEvents struct {
	renamed []string
	deleted []uint
	failed  []error
}

func withLibrary(t *testing.T, b *fakeLibrary, f func(d *LibraryDialog, ev *libraryEvents)) {
	t.Helper()
	uitest.OnUI(func() {
		ev := &libraryEvents{}
		d := NewLibraryDialog(context.Background(), nil, b, LibraryHooks{
			Renamed: func(id uint, title string) { ev.renamed = append(ev.renamed, title) },
			Deleted: func(id uint) { ev.deleted = append(ev.deleted, id) },
			Failed:  func(err error) { ev.failed = append(ev.failed, err) },
		})
		defer d.dlg.Delete()
		d.confirm = func(string) bool { return true }
		f(d, ev)
	})
}

func TestLibraryListsDocuments(t *testing.T) {
	withLibrary(t, newFakeLibrary(), func(d *LibraryDialog, _ *libraryEvents) {
		if d.list.Count() != 3 || !strings.HasPrefix(d.list.Item(0).Text(), "Essay on cats  ·  ") || !strings.HasPrefix(d.list.Item(2).Text(), "Notes  ·  ") {
			t.Errorf("rows: %d, first %q", d.list.Count(), d.list.Item(0).Text())
		}
		if d.list.CurrentRow() != 0 || !d.openBtn.IsEnabled() || !d.renameBtn.IsEnabled() || !d.deleteBtn.IsEnabled() {
			t.Error("the first document should be selected and the buttons enabled")
		}
		if _, ok := d.Opened(); ok {
			t.Error("nothing is opened until the user asks")
		}
	})
}

func TestLibraryEmpty(t *testing.T) {
	withLibrary(t, &fakeLibrary{}, func(d *LibraryDialog, _ *libraryEvents) {
		if d.list.Count() != 1 || !strings.Contains(d.list.Item(0).Text(), "library is empty") {
			t.Errorf("empty list shows %q", d.list.Item(0).Text())
		}
		if d.openBtn.IsEnabled() || d.renameBtn.IsEnabled() || d.deleteBtn.IsEnabled() {
			t.Error("with no documents the buttons are disabled")
		}
		d.openBtn.Click()
		if _, ok := d.Opened(); ok {
			t.Error("nothing to open")
		}
	})
}

func TestLibraryOpen(t *testing.T) {
	withLibrary(t, newFakeLibrary(), func(d *LibraryDialog, _ *libraryEvents) {
		d.list.SetCurrentRow(1)
		d.openBtn.Click()
		if id, ok := d.Opened(); !ok || id != 2 {
			t.Errorf("opened = %d, %v", id, ok)
		}
	})
	withLibrary(t, newFakeLibrary(), func(d *LibraryDialog, _ *libraryEvents) {
		d.list.ItemDoubleClicked(d.list.Item(2))
		d.list.SetCurrentRow(2)
		d.list.ItemDoubleClicked(d.list.Item(2))
		if id, ok := d.Opened(); !ok || id != 1 {
			t.Errorf("double click opened %d, %v", id, ok)
		}
	})
}

func TestLibraryRename(t *testing.T) {
	b := newFakeLibrary()
	withLibrary(t, b, func(d *LibraryDialog, ev *libraryEvents) {
		d.list.SetCurrentRow(1)
		asked := ""
		d.askTitle = func(current string) (string, bool) { asked = current; return "A better title", true }
		d.renameBtn.Click()
		if asked != "Letter" || b.renamed[2] != "A better title" || !slices.Equal(ev.renamed, []string{"A better title"}) {
			t.Errorf("asked %q, stored %v, hook %v", asked, b.renamed, ev.renamed)
		}
		if !strings.HasPrefix(d.list.Item(d.list.CurrentRow()).Text(), "A better title") {
			t.Error("the renamed row should stay selected")
		}

		for _, answer := range []struct {
			title string
			ok    bool
		}{{"", true}, {"A better title", true}, {"ignored", false}} {
			d.askTitle = func(string) (string, bool) { return answer.title, answer.ok }
			d.renameBtn.Click()
		}
		if len(ev.renamed) != 1 {
			t.Errorf("an empty, unchanged or cancelled title must not rename: %v", ev.renamed)
		}
	})
}

func TestLibraryDelete(t *testing.T) {
	b := newFakeLibrary()
	withLibrary(t, b, func(d *LibraryDialog, ev *libraryEvents) {
		d.confirm = func(text string) bool {
			if !strings.Contains(text, `"Essay on cats"`) {
				t.Errorf("confirmation = %q", text)
			}
			return false
		}
		d.deleteBtn.Click()
		if len(b.deleted) != 0 || len(ev.deleted) != 0 {
			t.Error("declining must delete nothing")
		}
		d.confirm = func(string) bool { return true }
		d.deleteBtn.Click()
		if !slices.Equal(b.deleted, []uint{3}) || !slices.Equal(ev.deleted, []uint{3}) || d.list.Count() != 2 {
			t.Errorf("deleted %v, hook %v, rows %d", b.deleted, ev.deleted, d.list.Count())
		}
		d.deleteBtn.Click()
		d.deleteBtn.Click()
		if d.openBtn.IsEnabled() || !strings.Contains(d.list.Item(0).Text(), "library is empty") {
			t.Error("deleting the last document shows the empty library")
		}
	})
}

func TestLibraryErrorsShowABanner(t *testing.T) {
	tests := []struct {
		name  string
		setup func(b *fakeLibrary)
		act   func(d *LibraryDialog)
	}{
		{"list", func(b *fakeLibrary) { b.listErr = errors.New("DatabaseQueryFailed", "database is locked") }, func(*LibraryDialog) {}},
		{"rename", func(b *fakeLibrary) { b.renameErr = errors.New("DocumentNotFound", "database is locked") }, func(d *LibraryDialog) {
			d.askTitle = func(string) (string, bool) { return "x", true }
			d.renameBtn.Click()
		}},
		{"delete", func(b *fakeLibrary) { b.deleteErr = errors.New("DocumentNotFound", "database is locked") }, func(d *LibraryDialog) { d.deleteBtn.Click() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newFakeLibrary()
			tt.setup(b)
			withLibrary(t, b, func(d *LibraryDialog, ev *libraryEvents) {
				tt.act(d)
				if d.banner.IsHidden() || !strings.Contains(d.banner.Text(), "database is locked") || len(ev.failed) == 0 {
					t.Errorf("banner hidden=%v text=%q failures=%d", d.banner.IsHidden(), d.banner.Text(), len(ev.failed))
				}
				if len(ev.renamed) != 0 || len(ev.deleted) != 0 {
					t.Error("a failed change must not be reported as done")
				}
			})
		})
	}
}

func TestModifiedTracking(t *testing.T) {
	withWindow(t, func(w *MainWindow) {
		var changes []bool
		w.SetHandlers(Handlers{ModifiedChanged: func(m bool) { changes = append(changes, m) }})
		if w.Modified() {
			t.Error("a new window is unmodified")
		}
		w.InsertTextAt(0, "hello")
		if !w.Modified() {
			t.Error("typing marks the text as modified")
		}
		w.SetModified(false)
		w.LoadText("loaded from a file")
		if w.Modified() || w.Text() != "loaded from a file" || w.CursorPosition() != 0 {
			t.Errorf("after LoadText: modified=%v text=%q cursor=%d", w.Modified(), w.Text(), w.CursorPosition())
		}
		if w.editor.Document().IsUndoAvailable() {
			t.Error("loading a text must not leave an undo step that would bring the old text back")
		}
		w.InsertTextAt(0, "x")
		w.ClearEditor()
		if w.Modified() || w.Text() != "" {
			t.Errorf("after ClearEditor: modified=%v text=%q", w.Modified(), w.Text())
		}
		if len(changes) == 0 || !slices.Contains(changes, true) || !slices.Contains(changes, false) {
			t.Errorf("modification changes = %v", changes)
		}
	})
}

// day is a fixed time in October 2026.
func day(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
