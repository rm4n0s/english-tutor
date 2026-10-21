package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// TestWindowDialogsReportCancelWhenDismissed opens each question the saving code
// asks and closes it without answering, as pressing Escape would. Closing a
// dialog must not count as confirming it.
func TestWindowDialogsReportCancelWhenDismissed(t *testing.T) {
	st := newStore(t)
	withAppTester(t, st, func(a *App, _ *syncBuffer, tester *gluttertest.Tester) {
		d := windowDialogs{a: a}
		var answered []string
		tests := []struct {
			name string
			ask  func()
		}{
			{"document title", func() {
				d.AskDocumentTitle("Title", func(_ string, ok bool) { answered = append(answered, strconv.FormatBool(ok)) })
			}},
			{"library", func() {
				d.ChooseDocument(func(_ uint, ok bool) { answered = append(answered, strconv.FormatBool(ok)) })
			}},
			{"unsaved changes", func() {
				d.AskUnsaved("essay.txt", func(c UnsavedChoice) { answered = append(answered, strconv.Itoa(int(c))) })
			}},
		}
		want := []string{"false", "false", strconv.Itoa(int(UnsavedCancel))}
		for i, tt := range tests {
			tt.ask()
			tester.PumpAndSettle()
			if err := dismissTop(tester); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			waitUntil(t, tester, func() bool { return len(answered) == i+1 })
			if answered[i] != want[i] {
				t.Errorf("%s: answered %s, want %s", tt.name, answered[i], want[i])
			}
		}
	})
}

// dismissTop closes the dialog that is on top of the window, the way Escape
// does.
func dismissTop(tester *gluttertest.Tester) error {
	tester.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	tester.PumpAndSettle()
	return nil
}

// waitUntil pumps the app until cond holds, giving goroutines up to five seconds
// to post their results.
func waitUntil(t *testing.T, tester *gluttertest.Tester, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tester.FlushPosted()
		tester.Pump()
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting")
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
