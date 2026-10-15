package store

import (
	"context"
	"testing"
	"time"
)

func TestDocumentCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	first := &Document{Title: "First", Content: "Hello world."}
	second := &Document{Title: "Second", Content: "Another text."}
	if err := s.CreateDocument(ctx, first); err != nil {
		t.Fatal(err)
	}
	// updated_at has second resolution in some drivers; make the order certain.
	time.Sleep(10 * time.Millisecond)
	if err := s.CreateDocument(ctx, second); err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || second.ID == 0 || first.ID == second.ID {
		t.Fatalf("IDs = %d, %d", first.ID, second.ID)
	}

	list, err := s.ListDocuments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "Second" || list[1].Title != "First" {
		t.Fatalf("ListDocuments() = %+v, want newest first", list)
	}

	time.Sleep(10 * time.Millisecond)
	first.Title = "First v2"
	first.Content = "Hello again."
	if err := s.UpdateDocument(ctx, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "First v2" || got.Content != "Hello again." {
		t.Errorf("GetDocument() = %+v", got)
	}
	list, err = s.ListDocuments(ctx)
	if err != nil || list[0].ID != first.ID {
		t.Errorf("after update the edited document should be first: %+v, %v", list, err)
	}

	if err := s.DeleteDocument(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDocument(ctx, first.ID); err == nil {
		t.Error("GetDocument() after delete should fail")
	}
}

func TestDocumentFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		store func(t *testing.T) *Store
		run   func(s *Store) error
		route string
	}{
		{"update missing", newTestStore, func(s *Store) error { return s.UpdateDocument(ctx, &Document{ID: 9, Title: "t"}) }, "Store.UpdateDocument.DocumentNotFound"},
		{"get missing", newTestStore, func(s *Store) error { _, err := s.GetDocument(ctx, 9); return err }, "Store.GetDocument.DocumentNotFound"},
		{"delete missing", newTestStore, func(s *Store) error { return s.DeleteDocument(ctx, 9) }, "Store.DeleteDocument.DocumentNotFound"},
		{"create query", closedStore, func(s *Store) error { return s.CreateDocument(ctx, &Document{Title: "t"}) }, "Store.CreateDocument.DatabaseQueryFailed"},
		{"update query", closedStore, func(s *Store) error { return s.UpdateDocument(ctx, &Document{ID: 1, Title: "t"}) }, "Store.UpdateDocument.DatabaseQueryFailed"},
		{"get query", closedStore, func(s *Store) error { _, err := s.GetDocument(ctx, 1); return err }, "Store.GetDocument.DatabaseQueryFailed"},
		{"list query", closedStore, func(s *Store) error { _, err := s.ListDocuments(ctx); return err }, "Store.ListDocuments.DatabaseQueryFailed"},
		{"delete query", closedStore, func(s *Store) error { return s.DeleteDocument(ctx, 1) }, "Store.DeleteDocument.DatabaseQueryFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRoute(t, tt.run(tt.store(t)), tt.route)
		})
	}
}
