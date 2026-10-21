package app

import (
	"context"
	"github.com/rm4n0s/english-tutor/internal/view"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

type fakeDialogs struct {
	openPath string
	openOK   bool

	savePath         string
	saveOK           bool
	saveSuggestions  []string
	unsaved          UnsavedChoice
	unsavedAsked     []string
	title            string
	titleOK          bool
	titleSuggestions []string
	docID            uint
	docOK            bool
}

func (f *fakeDialogs) ChooseOpenFile() (string, bool) { return f.openPath, f.openOK }
func (f *fakeDialogs) ChooseSaveFile(suggested string) (string, bool) {
	f.saveSuggestions = append(f.saveSuggestions, suggested)
	return f.savePath, f.saveOK
}
func (f *fakeDialogs) AskUnsaved(name string) UnsavedChoice {
	f.unsavedAsked = append(f.unsavedAsked, name)
	return f.unsaved
}
func (f *fakeDialogs) AskDocumentTitle(suggested string) (string, bool) {
	f.titleSuggestions = append(f.titleSuggestions, suggested)
	return f.title, f.titleOK
}
func (f *fakeDialogs) ChooseDocument() (uint, bool) { return f.docID, f.docOK }

// savingHarness is a harness whose dialogs are scripted.
func newSavingHarness(t *testing.T, replies ...llmtest.Reply) (*harness, *fakeDialogs) {
	t.Helper()
	h := newHarness(t, llmtest.New(replies...))
	fd := &fakeDialogs{}
	onUI(func() { h.a.dialogs = fd })
	return h, fd
}

// typed puts text in the editor as if the user had typed it: setPlainText would
// reset the modified flag, typing sets it.
func (h *harness) typed(text string) {
	h.t.Helper()
	onUI(func() {
		h.a.win.SetText(text)
		h.a.win.SetModified(true)
	})
}

func (h *harness) text() (s string) {
	onUI(func() { s = h.a.win.Text() })
	return s
}

func (h *harness) title() (s string) {
	onUI(func() { s = h.a.win.Widget().WindowTitle() })
	return s
}

func (h *harness) modified() (m bool) {
	onUI(func() { m = h.a.win.Modified() })
	return m
}

func TestWindowTitle(t *testing.T) {
	tests := []struct {
		name     string
		modified bool
		want     string
	}{
		{"essay.txt", false, "essay.txt — English Tutor"},
		{"essay.txt", true, "essay.txt* — English Tutor"},
		{untitled, true, "Untitled* — English Tutor"},
	}
	for _, tt := range tests {
		if got := windowTitle(tt.name, tt.modified); got != tt.want {
			t.Errorf("windowTitle(%q, %v) = %q, want %q", tt.name, tt.modified, got, tt.want)
		}
	}
}

func TestDefaultDocumentTitle(t *testing.T) {
	tests := []struct{ text, want string }{
		{"My holiday\nWe went to the sea.", "My holiday"},
		{"\n\n  \nSecond line is the first with text\nthird", "Second line is the first with text"},
		{"   padded   with   spaces   ", "padded with spaces"},
		{strings.Repeat("a", 100), strings.Repeat("a", 59) + "…"},
		{"", untitled},
		{" \n \t\n", untitled},
		{"日本語の題名\nbody", "日本語の題名"},
	}
	for _, tt := range tests {
		if got := defaultDocumentTitle(tt.text); got != tt.want {
			t.Errorf("defaultDocumentTitle(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestTitleFollowsModification(t *testing.T) {
	h, _ := newSavingHarness(t)
	if got := h.title(); got != "Untitled — English Tutor" {
		t.Errorf("title = %q", got)
	}
	h.typed("hello")
	if got := h.title(); got != "Untitled* — English Tutor" {
		t.Errorf("title = %q", got)
	}
}

func TestNewClearsEverything(t *testing.T) {
	h, fd := newSavingHarness(t, twoIssues())
	h.checked(nil)
	var oldKey string
	onUI(func() {
		oldKey = h.a.sourceKey
		h.a.win.SetModified(true) // as if the user had edited the checked text
	})
	fd.unsaved = UnsavedDiscard

	onUI(func() { h.a.onNew() })
	onUI(func() {
		if h.a.win.Text() != "" || h.a.win.Modified() || len(h.a.state.Items()) != 0 || h.a.state.Phase() != issues.NotChecked {
			t.Errorf("text %q modified %v items %d phase %v", h.a.win.Text(), h.a.win.Modified(), len(h.a.state.Items()), h.a.state.Phase())
		}
		if h.a.sourceKey == oldKey || !strings.HasPrefix(h.a.sourceKey, "session:") {
			t.Errorf("a new text starts a new session source, got %q (was %q)", h.a.sourceKey, oldKey)
		}
		if h.a.win.IssuesEmptyText() != "Press Check to find errors" || len(h.a.win.Underlines()) != 0 {
			t.Error("the list and the underlines must be empty")
		}
		if h.a.win.StatusText() != "Ready" {
			t.Errorf("status = %q", h.a.win.StatusText())
		}
	})
	if len(fd.unsavedAsked) != 1 || fd.unsavedAsked[0] != "Untitled" {
		t.Errorf("asked = %v: the text had unsaved changes", fd.unsavedAsked)
	}
}

func TestNewAsksOnlyWhenThereAreUnsavedChanges(t *testing.T) {
	h, fd := newSavingHarness(t)
	onUI(func() { h.a.onNew() })
	if len(fd.unsavedAsked) != 0 {
		t.Errorf("asked %v about an untouched text", fd.unsavedAsked)
	}
}

func TestUnsavedChoices(t *testing.T) {
	tests := []struct {
		name       string
		choice     UnsavedChoice
		wantText   string
		wantSaved  bool
		wantAsked  int
		wantSaveAs int
	}{
		{"cancel keeps the text", UnsavedCancel, "my draft", false, 1, 0},
		{"discard drops it", UnsavedDiscard, "", false, 1, 0},
		{"save asks where, then clears", UnsavedSave, "", true, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fd := newSavingHarness(t)
			path := filepath.Join(t.TempDir(), "draft.txt")
			fd.unsaved, fd.savePath, fd.saveOK = tt.choice, path, true
			h.typed("my draft")
			onUI(func() { h.a.onNew() })
			if got := h.text(); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
			data, err := os.ReadFile(path)
			if (err == nil) != tt.wantSaved || (tt.wantSaved && string(data) != "my draft") {
				t.Errorf("file saved = %v (%q, %v), want %v", err == nil, data, err, tt.wantSaved)
			}
			if len(fd.unsavedAsked) != tt.wantAsked || len(fd.saveSuggestions) != tt.wantSaveAs {
				t.Errorf("asked %d, save dialogs %d", len(fd.unsavedAsked), len(fd.saveSuggestions))
			}
		})
	}
}

func TestSavingBeforeNewStaysIfTheSaveIsCancelled(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.unsaved, fd.saveOK = UnsavedSave, false // the user changes their mind in the file dialog
	h.typed("precious")
	onUI(func() { h.a.onNew() })
	if h.text() != "precious" {
		t.Error("the text must stay when the save is cancelled")
	}
}

func TestOpenLoadsAFile(t *testing.T) {
	h, fd := newSavingHarness(t)
	path := writeFile(t, "essay.txt", []byte("My essay.\r\nSecond line."))
	fd.openPath, fd.openOK = path, true
	var oldKey string
	onUI(func() { oldKey = h.a.sourceKey })

	onUI(func() { h.a.onOpen() })
	onUI(func() {
		if h.a.win.Text() != "My essay.\nSecond line." || h.a.win.Modified() {
			t.Errorf("text %q modified %v", h.a.win.Text(), h.a.win.Modified())
		}
		if h.a.sourceKey != "file:"+path || h.a.sourceKey == oldKey {
			t.Errorf("source = %q, want file:%s", h.a.sourceKey, path)
		}
		if got := h.a.win.Widget().WindowTitle(); got != "essay.txt — English Tutor" {
			t.Errorf("title = %q", got)
		}
		if h.a.win.CursorPosition() != 0 {
			t.Error("the cursor starts at the beginning")
		}
	})
}

func TestOpenedFileIsSavedWithItsOwnLineEnding(t *testing.T) {
	h, fd := newSavingHarness(t)
	path := writeFile(t, "w.txt", []byte("one\r\ntwo\r\n"))
	fd.openPath, fd.openOK = path, true
	onUI(func() { h.a.onOpen() })
	onUI(func() { h.a.win.InsertTextAt(0, "zero\n"); h.a.onSave() })
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "zero\r\none\r\ntwo\r\n" {
		t.Errorf("file = %q, %v", got, err)
	}
	if h.modified() || h.title() != "w.txt — English Tutor" {
		t.Errorf("modified %v, title %q", h.modified(), h.title())
	}
}

func TestOpenFailuresKeepTheCurrentText(t *testing.T) {
	tests := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{"not utf-8", func(t *testing.T) string { return writeFile(t, "x.txt", []byte("caf\xe9")) }, "not valid UTF-8"},
		{"missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone.txt") }, "could not be read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fd := newSavingHarness(t)
			fd.openPath, fd.openOK = tt.path(t), true
			fd.unsaved = UnsavedCancel
			h.typed("unsaved work")
			onUI(func() { h.a.onOpen() })
			onUI(func() {
				if h.a.win.Text() != "unsaved work" || !h.a.win.Modified() {
					t.Error("a file that cannot be opened must not replace the text")
				}
				if got := h.a.win.ErrorText(); !strings.Contains(got, tt.want) {
					t.Errorf("banner = %q, want it to contain %q", got, tt.want)
				}
			})
			if len(fd.unsavedAsked) != 0 {
				t.Errorf("the user must not be asked about unsaved changes for a file that fails to open, asked %v", fd.unsavedAsked)
			}
			if !strings.Contains(h.logs.String(), "level=ERROR") {
				t.Errorf("the failure must be logged:\n%s", h.logs.String())
			}
		})
	}
}

func TestOpenCancelledChangesNothing(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.openOK = false
	h.typed("work")
	onUI(func() { h.a.onOpen() })
	if h.text() != "work" || len(fd.unsavedAsked) != 0 {
		t.Error("cancelling the file dialog changes nothing and asks nothing")
	}
}

func TestOpenAsksAboutUnsavedChangesAfterTheFileIsRead(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.openPath, fd.openOK = writeFile(t, "a.txt", []byte("from file")), true
	fd.unsaved = UnsavedCancel
	h.typed("work")
	onUI(func() { h.a.onOpen() })
	if h.text() != "work" || len(fd.unsavedAsked) != 1 {
		t.Errorf("text %q, asked %v", h.text(), fd.unsavedAsked)
	}
	fd.unsaved = UnsavedDiscard
	onUI(func() { h.a.onOpen() })
	if h.text() != "from file" {
		t.Errorf("text = %q", h.text())
	}
}

func TestSaveWithoutAFileBehavesLikeSaveAs(t *testing.T) {
	h, fd := newSavingHarness(t)
	path := filepath.Join(t.TempDir(), "essay.md")
	fd.savePath, fd.saveOK = path, true
	h.typed("My holiday\nWe went to the sea.")
	onUI(func() { h.a.onSave() })

	if got, err := os.ReadFile(path); err != nil || string(got) != "My holiday\nWe went to the sea." {
		t.Errorf("file = %q, %v", got, err)
	}
	if len(fd.saveSuggestions) != 1 || fd.saveSuggestions[0] != "My holiday.txt" {
		t.Errorf("suggested name = %v, want the first line plus .txt", fd.saveSuggestions)
	}
	if h.modified() || h.title() != "essay.md — English Tutor" {
		t.Errorf("modified %v, title %q", h.modified(), h.title())
	}

	// A second Save writes to the same file without asking.
	onUI(func() { h.a.win.InsertTextAt(0, "# ") })
	onUI(func() { h.a.onSave() })
	if len(fd.saveSuggestions) != 1 {
		t.Error("Save must not ask for a file again")
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "# My holiday") {
		t.Errorf("file = %q", got)
	}
}

func TestSaveAsAddsAnExtension(t *testing.T) {
	h, fd := newSavingHarness(t)
	base := filepath.Join(t.TempDir(), "noextension")
	fd.savePath, fd.saveOK = base, true
	h.typed("text")
	onUI(func() { h.a.onSaveAs() })
	if got, err := os.ReadFile(base + ".txt"); err != nil || string(got) != "text" {
		t.Errorf("file = %q, %v", got, err)
	}
}

func TestSaveAsCancelledChangesNothing(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.saveOK = false
	h.typed("text")
	onUI(func() { h.a.onSaveAs() })
	if !h.modified() || h.title() != "Untitled* — English Tutor" {
		t.Errorf("modified %v, title %q", h.modified(), h.title())
	}
}

func TestSaveFailureShowsABannerAndKeepsTheTextModified(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.savePath, fd.saveOK = filepath.Join(t.TempDir(), "missing-folder", "a.txt"), true
	h.typed("text")
	onUI(func() {
		h.a.onSaveAs()
		if !h.a.win.Modified() || !strings.Contains(h.a.win.ErrorText(), "could not be saved") {
			t.Errorf("modified %v, banner %q", h.a.win.Modified(), h.a.win.ErrorText())
		}
		if strings.HasPrefix(h.a.sourceKey, "file:") {
			t.Error("a failed save must not move the text to the new source")
		}
	})
}

func TestSaveAsMovesTheRecordedMistakes(t *testing.T) {
	h, fd := newSavingHarness(t, twoIssues())
	h.checked(nil)
	var sessionKey string
	onUI(func() { sessionKey = h.a.sourceKey })
	path := filepath.Join(t.TempDir(), "essay.txt")
	fd.savePath, fd.saveOK = path, true

	onUI(func() { h.a.onSaveAs() })
	var fileKey string
	onUI(func() { fileKey = h.a.sourceKey })
	if fileKey != "file:"+path {
		t.Fatalf("source = %q", fileKey)
	}
	if n := h.events(); n != 2 {
		t.Errorf("events = %d: Save As moves them, it does not copy or drop them", n)
	}
	// The events are now under the file's key: removing one by that key works,
	// and the old key has none.
	fingerprint := ""
	onUI(func() { fingerprint = h.a.state.Items()[0].Fingerprint })
	if err := h.a.stats.NotAnError(context.Background(), sessionKey, fingerprint); err != nil {
		t.Fatal(err)
	}
	if n := h.events(); n != 2 {
		t.Errorf("events = %d: nothing should be left under the session key", n)
	}
	if err := h.a.stats.NotAnError(context.Background(), fileKey, fingerprint); err != nil {
		t.Fatal(err)
	}
	if n := h.events(); n != 1 {
		t.Errorf("events = %d, want 1 after removing one under the file's key", n)
	}
}

func TestSaveToLibraryCreatesADocument(t *testing.T) {
	h, fd := newSavingHarness(t, twoIssues())
	h.checked(nil)
	fd.title, fd.titleOK = "Holiday essay", true
	onUI(func() { h.a.onSaveToLibrary() })

	docs, err := h.st.ListDocuments(context.Background())
	if err != nil || len(docs) != 1 || docs[0].Title != "Holiday essay" || docs[0].Content != sample {
		t.Fatalf("documents = %+v, %v", docs, err)
	}
	if len(fd.titleSuggestions) != 1 || fd.titleSuggestions[0] != "He go home. She have a cat." {
		t.Errorf("suggested title = %v, want the first line", fd.titleSuggestions)
	}
	onUI(func() {
		if h.a.sourceKey != "doc:"+itoa(docs[0].ID) || h.a.win.Modified() {
			t.Errorf("source %q modified %v", h.a.sourceKey, h.a.win.Modified())
		}
		if got := h.a.win.Widget().WindowTitle(); got != "Holiday essay — English Tutor" {
			t.Errorf("title = %q", got)
		}
	})
	if n := h.events(); n != 2 {
		t.Errorf("events = %d: they follow the text to the library", n)
	}

	// The next Save to library updates that document instead of adding another.
	fd.titleOK = false // no title would be asked anyway
	onUI(func() { h.a.win.InsertTextAt(0, "Edited. "); h.a.onSaveToLibrary() })
	docs, _ = h.st.ListDocuments(context.Background())
	if len(docs) != 1 || !strings.HasPrefix(docs[0].Content, "Edited. ") || docs[0].Title != "Holiday essay" {
		t.Errorf("documents = %+v", docs)
	}
	if len(fd.titleSuggestions) != 1 {
		t.Error("an existing document keeps its title and is not asked for one")
	}
}

func TestSaveToLibraryCancelledAddsNothing(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.titleOK = false
	h.typed("text")
	onUI(func() { h.a.onSaveToLibrary() })
	if docs, _ := h.st.ListDocuments(context.Background()); len(docs) != 0 || !h.modified() {
		t.Errorf("documents = %+v, modified %v", docs, h.modified())
	}
}

func TestSaveToLibraryFromAFileMovesTheSource(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.openPath, fd.openOK = writeFile(t, "a.txt", []byte("from a file")), true
	onUI(func() { h.a.onOpen() })
	fd.title, fd.titleOK = "Kept", true
	onUI(func() { h.a.win.InsertTextAt(0, "x"); h.a.onSaveToLibrary() })
	onUI(func() {
		if !strings.HasPrefix(h.a.sourceKey, "doc:") {
			t.Errorf("source = %q: the text's identity is where it was most recently saved", h.a.sourceKey)
		}
		if h.a.place.filePath == "" {
			t.Error("the file association stays, so Save still writes the file")
		}
	})
}

func TestOpenFromTheLibraryAndSaveBack(t *testing.T) {
	h, fd := newSavingHarness(t)
	doc := &store.Document{Title: "Letter", Content: "Dear Sir,\nthanks."}
	if err := h.st.CreateDocument(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	fd.docID, fd.docOK = doc.ID, true
	onUI(func() { h.a.onLibrary() })
	onUI(func() {
		if h.a.win.Text() != "Dear Sir,\nthanks." || h.a.win.Modified() || h.a.sourceKey != "doc:"+itoa(doc.ID) {
			t.Errorf("text %q modified %v source %q", h.a.win.Text(), h.a.win.Modified(), h.a.sourceKey)
		}
		if h.a.win.Widget().WindowTitle() != "Letter — English Tutor" {
			t.Errorf("title = %q", h.a.win.Widget().WindowTitle())
		}
	})
	onUI(func() { h.a.win.InsertTextAt(0, "PS "); h.a.onSaveToLibrary() })
	docs, _ := h.st.ListDocuments(context.Background())
	if len(docs) != 1 || !strings.HasPrefix(docs[0].Content, "PS Dear") || len(fd.titleSuggestions) != 0 {
		t.Errorf("documents %+v, title questions %v: the open document is updated, not duplicated", docs, fd.titleSuggestions)
	}
}

func TestLibraryOpenAsksAboutUnsavedChanges(t *testing.T) {
	h, fd := newSavingHarness(t)
	doc := &store.Document{Title: "Letter", Content: "Dear Sir"}
	if err := h.st.CreateDocument(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	fd.docID, fd.docOK, fd.unsaved = doc.ID, true, UnsavedCancel
	h.typed("work in progress")
	onUI(func() { h.a.onLibrary() })
	if h.text() != "work in progress" || len(fd.unsavedAsked) != 1 {
		t.Errorf("text %q, asked %v", h.text(), fd.unsavedAsked)
	}
}

func TestLibraryDocumentThatVanishedShowsAnError(t *testing.T) {
	h, fd := newSavingHarness(t)
	fd.docID, fd.docOK = 999, true
	h.typed("work")
	onUI(func() {
		h.a.onLibrary()
		if h.a.win.ErrorText() == "" || h.a.win.Text() != "work" {
			t.Errorf("banner %q text %q", h.a.win.ErrorText(), h.a.win.Text())
		}
	})
}

func TestRenamingAndDeletingTheOpenDocument(t *testing.T) {
	h, fd := newSavingHarness(t)
	doc := &store.Document{Title: "Letter", Content: "Dear Sir"}
	if err := h.st.CreateDocument(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	fd.docID, fd.docOK = doc.ID, true
	onUI(func() { h.a.onLibrary() })

	onUI(func() {
		h.a.onDocumentRenamed(doc.ID+1, "other") // another document: ignored
		h.a.onDocumentRenamed(doc.ID, "Better title")
		if got := h.a.win.Widget().WindowTitle(); got != "Better title — English Tutor" {
			t.Errorf("title = %q", got)
		}
		h.a.onDocumentDeleted(doc.ID + 1)
		if h.a.place.docID != doc.ID {
			t.Error("deleting another document must not detach this one")
		}
		h.a.onDocumentDeleted(doc.ID)
		if h.a.place.docID != 0 {
			t.Error("deleting the open document detaches it")
		}
	})
	// The next Save to library creates a new document.
	if err := h.st.DeleteDocument(context.Background(), doc.ID); err != nil {
		t.Fatal(err)
	}
	fd.title, fd.titleOK = "Second copy", true
	onUI(func() { h.a.win.InsertTextAt(0, "x"); h.a.onSaveToLibrary() })
	docs, _ := h.st.ListDocuments(context.Background())
	if len(docs) != 1 || docs[0].Title != "Second copy" {
		t.Errorf("documents = %+v", docs)
	}
}

func TestSavingToADocumentDeletedMeanwhile(t *testing.T) {
	h, fd := newSavingHarness(t)
	doc := &store.Document{Title: "Letter", Content: "Dear Sir"}
	if err := h.st.CreateDocument(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	fd.docID, fd.docOK = doc.ID, true
	onUI(func() { h.a.onLibrary() })
	if err := h.st.DeleteDocument(context.Background(), doc.ID); err != nil { // deleted from somewhere else
		t.Fatal(err)
	}
	onUI(func() {
		h.a.win.InsertTextAt(0, "x")
		h.a.onSaveToLibrary()
		if !strings.Contains(h.a.win.ErrorText(), "no longer exists") || !h.a.win.Modified() || h.a.place.docID != 0 {
			t.Errorf("banner %q modified %v docID %d", h.a.win.ErrorText(), h.a.win.Modified(), h.a.place.docID)
		}
	})
}

func TestClosingAsksAboutUnsavedChanges(t *testing.T) {
	tests := []struct {
		name   string
		choice UnsavedChoice
		want   bool
	}{{"cancel keeps the window open", UnsavedCancel, false}, {"discard closes", UnsavedDiscard, true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fd := newSavingHarness(t)
			fd.unsaved = tt.choice
			h.typed("draft")
			onUI(func() {
				if got := h.a.canClose(); got != tt.want {
					t.Errorf("canClose() = %v, want %v", got, tt.want)
				}
			})
		})
	}
	h, fd := newSavingHarness(t)
	onUI(func() {
		if !h.a.canClose() || len(fd.unsavedAsked) != 0 {
			t.Error("an unchanged text closes without a question")
		}
	})
}

func TestClosingSavesWhereTheTextBelongs(t *testing.T) {
	t.Run("a file", func(t *testing.T) {
		h, fd := newSavingHarness(t)
		path := writeFile(t, "a.txt", []byte("old"))
		fd.openPath, fd.openOK, fd.unsaved = path, true, UnsavedSave
		onUI(func() { h.a.onOpen() })
		onUI(func() { h.a.win.InsertTextAt(3, " and new"); h.a.canClose() })
		if got, _ := os.ReadFile(path); string(got) != "old and new" || len(fd.saveSuggestions) != 0 {
			t.Errorf("file %q, save dialogs %v: Save writes to the open file", got, fd.saveSuggestions)
		}
	})
	t.Run("a library document", func(t *testing.T) {
		h, fd := newSavingHarness(t)
		doc := &store.Document{Title: "Letter", Content: "Dear"}
		if err := h.st.CreateDocument(context.Background(), doc); err != nil {
			t.Fatal(err)
		}
		fd.docID, fd.docOK, fd.unsaved = doc.ID, true, UnsavedSave
		onUI(func() { h.a.onLibrary() })
		onUI(func() { h.a.win.InsertTextAt(4, " Sir"); h.a.canClose() })
		if got, _ := h.st.GetDocument(context.Background(), doc.ID); got.Content != "Dear Sir" || len(fd.saveSuggestions) != 0 {
			t.Errorf("document %+v, save dialogs %v: a library text is saved to the library", got, fd.saveSuggestions)
		}
	})
}

func TestNewStopsARunningCheck(t *testing.T) {
	h, fd := newSavingHarness(t, llmtest.Reply{Text: issuesReply(map[string]string{"He go": "verb_form"}), Delay: 5 * time.Second})
	fd.unsaved = UnsavedDiscard
	h.typed(sample)
	h.checkButton()
	h.waitFor("the request", func() bool { return h.a.checking })
	onUI(func() { h.a.onNew() })
	onUI(func() {
		if h.a.checking || h.a.win.CheckButtonText() != "Check" || h.a.win.StatusText() != "Ready" {
			t.Errorf("checking %v, button %q, status %q", h.a.checking, h.a.win.CheckButtonText(), h.a.win.StatusText())
		}
	})
	time.Sleep(50 * time.Millisecond) // the cancelled check reports meanwhile
	onUI(func() {
		if h.a.win.StatusText() != "Ready" || len(h.a.win.Underlines()) != 0 {
			t.Errorf("a result for the old text must be ignored: status %q underlines %v", h.a.win.StatusText(), h.a.win.Underlines())
		}
	})
}

func TestLoadingATextIsNotTypingForAutoCheck(t *testing.T) {
	fake := llmtest.New(twoIssues())
	h := newHarness(t, fake)
	fd := &fakeDialogs{openPath: writeFile(t, "a.txt", []byte(sample)), openOK: true}
	onUI(func() {
		h.a.dialogs = fd
		h.a.autoDelay = 20 * time.Millisecond
		h.a.onAutoCheckChanged(true)
		h.a.onOpen()
	})
	time.Sleep(120 * time.Millisecond)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("calls = %d: opening a file must not start an auto-check", n)
	}
}

func TestWindowStateIsConsistentAfterOpen(t *testing.T) {
	h, fd := newSavingHarness(t, twoIssues())
	h.checked(nil)
	h.selectIssue(1)
	fd.openPath, fd.openOK, fd.unsaved = writeFile(t, "a.txt", []byte("fresh")), true, UnsavedDiscard
	onUI(func() { h.a.onOpen() })
	onUI(func() {
		if h.a.win.ExplanationState() != view.ExplainIdle || h.a.win.SelectedIssueRow() != 0 || len(h.a.win.IssueRowIDs()) != 0 {
			t.Errorf("panel %v, selected row %d, rows %v", h.a.win.ExplanationState(), h.a.win.SelectedIssueRow(), h.a.win.IssueRowIDs())
		}
	})
}

func itoa(n uint) string { return strconv.FormatUint(uint64(n), 10) }
