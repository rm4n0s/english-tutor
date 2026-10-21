package gui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// inDialog finds text inside the dialog on top, not in the window under it.
func inDialog(text string) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByType[*m.Dialog](), gluttertest.ByText(text))
}

func tapInDialog(t *testing.T, tester *gluttertest.Tester, text string) {
	t.Helper()
	if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
		t.Fatalf("tapping %q in the dialog: %v", text, err)
	}
	tester.Tap(inDialog(text))
	tester.Pump()
}

func TestChooseReportsTheButtonPressed(t *testing.T) {
	tester, w := pumpWindow(t)
	got := -1
	w.Choose("Unsaved changes", "Do you want to save the changes to Essay?", []string{"Save", "Discard", "Cancel"}, 2, func(i int) { got = i })
	tester.PumpAndSettle()
	for _, text := range []string{"Unsaved changes", "Do you want to save the changes to Essay?", "Save", "Discard", "Cancel"} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	tapInDialog(t, tester, "Discard")
	tester.PumpAndSettle()
	if got != 1 {
		t.Errorf("answer = %d, want 1", got)
	}
	if err := gluttertest.FindsNothing(tester, gluttertest.ByText("Discard")); err != nil {
		t.Errorf("the dialog should be gone: %v", err)
	}
}

func TestChooseReportsCancelWhenDismissed(t *testing.T) {
	tester, w := pumpWindow(t)
	answers := []int{}
	d := w.Choose("Q", "text", []string{"Yes", "No"}, 1, func(i int) { answers = append(answers, i) })
	tester.PumpAndSettle()
	d.Close()
	tester.PumpAndSettle()
	tester.Pump(time.Millisecond)
	if !slices.Equal(answers, []int{1}) {
		t.Errorf("answers = %v, want one cancel", answers)
	}
}

func TestConfirm(t *testing.T) {
	tester, w := pumpWindow(t)
	var got []bool
	w.Confirm("Library", "Delete it?", func(yes bool) { got = append(got, yes) })
	tester.PumpAndSettle()
	tapInDialog(t, tester, "Yes")
	tester.PumpAndSettle()
	w.Confirm("Library", "Delete it?", func(yes bool) { got = append(got, yes) })
	tester.PumpAndSettle()
	tapInDialog(t, tester, "No")
	tester.PumpAndSettle()
	if !slices.Equal(got, []bool{true, false}) {
		t.Errorf("answers = %v", got)
	}
}

func TestAskTextReturnsWhatWasTyped(t *testing.T) {
	tester, w := pumpWindow(t)
	var text string
	var ok bool
	d := w.AskText("Save to library", "Title", "Essay", func(s string, k bool) { text, ok = s, k })
	tester.PumpAndSettle()
	if d.field.Text() != "Essay" {
		t.Fatalf("the field starts with %q", d.field.Text())
	}
	d.field.SetText("A new title")
	tapInDialog(t, tester, "OK")
	tester.PumpAndSettle()
	if text != "A new title" || !ok {
		t.Errorf("answer = %q, %v", text, ok)
	}
	ok = true
	w.AskText("Save to library", "Title", "x", func(s string, k bool) { text, ok = s, k })
	tester.PumpAndSettle()
	tapInDialog(t, tester, "Cancel")
	tester.PumpAndSettle()
	if ok {
		t.Error("Cancel must report ok = false")
	}
}

// ---- Library ----

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

func day(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

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
	done    []uint
	closed  int
}

func showLibrary(t *testing.T, b *fakeLibrary) (*gluttertest.Tester, *LibraryDialog, *libraryEvents) {
	t.Helper()
	tester, w := pumpWindow(t)
	ev := &libraryEvents{}
	d := NewLibraryDialog(context.Background(), w, b, LibraryHooks{
		Renamed: func(id uint, title string) { ev.renamed = append(ev.renamed, title) },
		Deleted: func(id uint) { ev.deleted = append(ev.deleted, id) },
		Failed:  func(err error) { ev.failed = append(ev.failed, err) },
		Done: func(id uint, ok bool) {
			ev.closed++
			if ok {
				ev.done = append(ev.done, id)
			}
		},
	})
	d.confirm = func(_ string, done func(bool)) { done(true) }
	w.ShowDialog(d)
	tester.PumpAndSettle()
	return tester, d, ev
}

func TestLibraryListsDocuments(t *testing.T) {
	tester, d, _ := showLibrary(t, newFakeLibrary())
	rows := d.Rows()
	if len(rows) != 3 || !strings.HasPrefix(rows[0], "Essay on cats  ·  ") || !strings.HasPrefix(rows[2], "Notes  ·  ") {
		t.Errorf("rows = %q", rows)
	}
	if d.SelectedRow() != 0 || !d.ButtonsEnabled() {
		t.Error("the first document should be selected and the buttons enabled")
	}
	if _, ok := d.Opened(); ok {
		t.Error("nothing is opened until the user asks")
	}
	for _, text := range []string{"Library", "Open", "Rename...", "Delete", "Close"} {
		if err := gluttertest.FindsOneWidget(tester, inDialog(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("Essay on cats")); err != nil {
		t.Error(err)
	}
}

func TestLibraryEmpty(t *testing.T) {
	tester, d, ev := showLibrary(t, &fakeLibrary{})
	if rows := d.Rows(); len(rows) != 1 || !strings.Contains(rows[0], "library is empty") || d.ButtonsEnabled() {
		t.Errorf("rows %q, enabled %v", rows, d.ButtonsEnabled())
	}
	d.Open()
	if _, ok := d.Opened(); ok {
		t.Error("nothing to open")
	}
	tapInDialog(t, tester, "Close")
	tester.PumpAndSettle()
	if ev.closed != 1 || len(ev.done) != 0 {
		t.Errorf("closed %d, chosen %v", ev.closed, ev.done)
	}
}

func TestLibraryOpenByButtonAndByDoubleTap(t *testing.T) {
	tester, d, ev := showLibrary(t, newFakeLibrary())
	d.SelectRow(1)
	tester.Pump()
	tapInDialog(t, tester, "Open")
	tester.PumpAndSettle()
	if !slices.Equal(ev.done, []uint{2}) {
		t.Errorf("Open chose %v", ev.done)
	}

	tester, _, ev = showLibrary(t, newFakeLibrary())
	f := gluttertest.ByTextContaining("Notes")
	tester.Tap(f)
	tester.Pump(40 * time.Millisecond)
	tester.Tap(f)
	tester.PumpAndSettle()
	if !slices.Equal(ev.done, []uint{1}) {
		t.Errorf("a double tap chose %v", ev.done)
	}
}

func TestLibraryRename(t *testing.T) {
	b := newFakeLibrary()
	_, d, ev := showLibrary(t, b)
	d.SelectRow(1)
	asked := ""
	d.askTitle = func(current string, done func(string, bool)) { asked = current; done("A better title", true) }
	d.Rename()
	if asked != "Letter" || b.renamed[2] != "A better title" || !slices.Equal(ev.renamed, []string{"A better title"}) {
		t.Errorf("asked %q, stored %v, hook %v", asked, b.renamed, ev.renamed)
	}
	if !strings.HasPrefix(d.Rows()[d.SelectedRow()], "A better title") {
		t.Error("the renamed row should stay selected")
	}
	for _, answer := range []struct {
		title string
		ok    bool
	}{{"", true}, {"A better title", true}, {"Other", false}} {
		d.askTitle = func(string, func(string, bool)) {}
		d.askTitle = func(_ string, done func(string, bool)) { done(answer.title, answer.ok) }
		d.Rename()
	}
	if len(b.renamed) != 1 {
		t.Errorf("an empty, unchanged or cancelled title must not rename: %v", b.renamed)
	}
}

func TestLibraryDelete(t *testing.T) {
	b := newFakeLibrary()
	_, d, ev := showLibrary(t, b)
	d.SelectRow(1)
	var asked string
	d.confirm = func(text string, done func(bool)) { asked = text; done(false) }
	d.Delete()
	if len(b.deleted) != 0 || !strings.Contains(asked, `"Letter"`) {
		t.Errorf("declining deleted %v, asked %q", b.deleted, asked)
	}
	d.confirm = func(_ string, done func(bool)) { done(true) }
	d.Delete()
	if !slices.Equal(b.deleted, []uint{2}) || !slices.Equal(ev.deleted, []uint{2}) || len(d.Rows()) != 2 || d.SelectedRow() != 0 {
		t.Errorf("deleted %v hook %v rows %v selected %d", b.deleted, ev.deleted, d.Rows(), d.SelectedRow())
	}
}

func TestLibraryErrorsShowInTheDialogAndAreReported(t *testing.T) {
	b := newFakeLibrary()
	b.renameErr = errors.New("DocumentSaveFailed", "The title could not be saved.")
	b.deleteErr = errors.New("DocumentDeleteFailed", "The document could not be deleted.")
	_, d, ev := showLibrary(t, b)
	d.askTitle = func(_ string, done func(string, bool)) { done("New", true) }
	d.Rename()
	if d.BannerText() != "The title could not be saved." || len(ev.failed) != 1 {
		t.Errorf("banner %q failed %v", d.BannerText(), ev.failed)
	}
	d.Delete()
	if d.BannerText() != "The document could not be deleted." || len(ev.failed) != 2 || len(ev.deleted) != 0 {
		t.Errorf("banner %q failed %v deleted %v", d.BannerText(), ev.failed, ev.deleted)
	}
	b.renameErr = nil
	d.Rename()
	if d.BannerText() != "" {
		t.Errorf("a success clears the banner, got %q", d.BannerText())
	}

	failing := &fakeLibrary{listErr: errors.New("DocumentListFailed", "The library could not be read.")}
	_, d, ev = showLibrary(t, failing)
	if d.BannerText() != "The library could not be read." || len(ev.failed) != 1 {
		t.Errorf("banner %q failed %v", d.BannerText(), ev.failed)
	}
}

// ---- Alternatives ----

type altEvents struct {
	retries, closed int
	chosen          []string
	copied          []string
}

func showAlternatives(t *testing.T, selected string) (*gluttertest.Tester, *MainWindow, *AlternativesDialog, *altEvents) {
	t.Helper()
	tester, w := pumpWindow(t)
	ev := &altEvents{}
	d := NewAlternativesDialog(w, selected, AlternativesHooks{
		Retry:  func() { ev.retries++ },
		Chosen: func(text string) { ev.chosen = append(ev.chosen, text) },
		Closed: func() { ev.closed++ },
	})
	d.SetCopyAction(func(text string) { ev.copied = append(ev.copied, text) })
	d.Show()
	tester.Pump()
	return tester, w, d, ev
}

func TestAlternativesStartsLoading(t *testing.T) {
	tester, _, d, _ := showAlternatives(t, "he go")
	if !d.IsVisible() || !d.Loading() || d.StatusText() != "Looking for alternatives..." || d.SelectionText() != "he go" {
		t.Errorf("visible %v loading %v status %q selection %q", d.IsVisible(), d.Loading(), d.StatusText(), d.SelectionText())
	}
	if d.CopyEnabled() || d.RetryVisible() {
		t.Error("nothing to copy or retry yet")
	}
	for _, text := range []string{"Alternatives", "Selected text:", "he go", "Looking for alternatives..."} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(text)); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestAlternativesListAndChoose(t *testing.T) {
	tester, _, d, ev := showAlternatives(t, "he go")
	d.SetAlternatives([]string{"he goes", "he is going"})
	tester.Pump()
	if d.Loading() || d.StatusText() != "2 alternatives" || !d.CopyEnabled() || !slices.Equal(d.Alternatives(), []string{"he goes", "he is going"}) {
		t.Errorf("loading %v status %q alternatives %v", d.Loading(), d.StatusText(), d.Alternatives())
	}
	d.CopyClick()
	d.SelectRow(1)
	d.CopyClick()
	d.DoubleClickRow(0)
	d.DoubleClickRow(5)
	if !slices.Equal(ev.copied, []string{"he goes", "he is going"}) || !slices.Equal(ev.chosen, []string{"he goes"}) {
		t.Errorf("copied %v chosen %v", ev.copied, ev.chosen)
	}
	f := gluttertest.ByText("he is going")
	tester.Tap(f)
	tester.Pump(40 * time.Millisecond)
	tester.Tap(f)
	tester.Pump()
	if !slices.Equal(ev.chosen, []string{"he goes", "he is going"}) {
		t.Errorf("a double tap chose %v", ev.chosen)
	}
	d.SetAlternatives(nil)
	if d.StatusText() != "No alternatives found." || d.CopyEnabled() {
		t.Errorf("status %q", d.StatusText())
	}
}

func TestAlternativesFailureOffersRetry(t *testing.T) {
	tester, _, d, ev := showAlternatives(t, "x")
	d.SetFailed("The request failed: refused")
	tester.Pump()
	if d.ErrorText() != "The request failed: refused" || !d.RetryVisible() || d.Loading() {
		t.Errorf("error %q retry %v", d.ErrorText(), d.RetryVisible())
	}
	tapText(t, tester, "Retry")
	d.SetLoading()
	if ev.retries != 1 || d.RetryVisible() || d.ErrorText() != "" {
		t.Errorf("retries %d retry %v error %q", ev.retries, d.RetryVisible(), d.ErrorText())
	}
	d.SetAlternatives([]string{"a"})
	d.SetNotice("The text changed")
	if d.ErrorText() != "The text changed" || len(d.Alternatives()) != 1 || d.RetryVisible() {
		t.Errorf("a notice keeps the list: error %q alternatives %v", d.ErrorText(), d.Alternatives())
	}
}

func TestAlternativesCloseReportsOnce(t *testing.T) {
	tester, _, d, ev := showAlternatives(t, "x")
	tapText(t, tester, "×")
	d.Close()
	if d.IsVisible() || ev.closed != 1 {
		t.Errorf("visible %v closed %d", d.IsVisible(), ev.closed)
	}
}

func TestShortSelectionAndStatus(t *testing.T) {
	long := strings.Repeat("é", maxShownSelection+10)
	if got := ShortSelection(long); len([]rune(got)) != maxShownSelection+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("a long selection is cut with an ellipsis, got %d runes", len([]rune(got)))
	}
	if ShortSelection("short") != "short" {
		t.Error("a short selection is kept")
	}
	for n, want := range map[int]string{0: "No alternatives found.", 1: "1 alternative", 7: "7 alternatives"} {
		if got := AlternativesStatus(n); got != want {
			t.Errorf("AlternativesStatus(%d) = %q", n, got)
		}
	}
}
