package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

func TestOpenCreatesPrivateFileAndReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "english-tutor.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	cfg := validConfig("local")
	if err := s.CreateLLMConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	// Reopening runs the migrations again without error and keeps the data.
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := s.ListLLMConfigs(ctx)
	if err != nil || len(got) != 1 || got[0].Name != "local" {
		t.Errorf("ListLLMConfigs() = %v, %v; want the saved config", got, err)
	}
}

func TestOpenTightensExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "english-tutor.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
}

func TestOpenFailure(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing", "english-tutor.db")
	_, err := Open(context.Background(), missingDir)
	assertRoute(t, err, "Open.DatabaseOpenFailed")
}

func TestMigrationsAreRecorded(t *testing.T) {
	s := newTestStore(t)
	var ids []string
	if err := s.db.Model(&appliedMigration{}).Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{"001_init", "002_error_events", "003_llm_usage", "004_llm_request_settings", "005_llm_no_thinking"}
	if len(ids) != len(want) {
		t.Fatalf("applied = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("applied = %v, want %v", ids, want)
			break
		}
	}
	for _, table := range []string{"llm_configs", "documents", "settings", "error_events", "llm_usage"} {
		if !s.db.Migrator().HasTable(table) {
			t.Errorf("table %s was not created", table)
		}
	}
}

func TestRunMigrations(t *testing.T) {
	failing := errors.New("Inner", "boom")
	tests := []struct {
		name      string
		list      []migration
		wantRoute string
		wantApply []string
	}{
		{
			name:      "applies each migration once",
			list:      []migration{{id: "a", up: func(*gorm.DB) error { return nil }}},
			wantApply: []string{"001_init", "002_error_events", "003_llm_usage", "004_llm_request_settings", "005_llm_no_thinking", "a"},
		},
		{
			name: "failing migration is rolled back and reported",
			list: []migration{
				{id: "bad", up: func(*gorm.DB) error { return failing }},
			},
			wantRoute: "runMigrations.MigrationFailed",
			wantApply: []string{"001_init", "002_error_events", "003_llm_usage", "004_llm_request_settings", "005_llm_no_thinking"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			err := s.runMigrations(ctx, tt.list)
			if tt.wantRoute != "" {
				assertRoute(t, err, tt.wantRoute)
			} else if err != nil {
				t.Fatal(err)
			}
			// Running the same list again must not apply anything twice.
			if tt.wantRoute == "" {
				if err := s.runMigrations(ctx, tt.list); err != nil {
					t.Fatalf("second run: %v", err)
				}
			}
			var ids []string
			if err := s.db.Model(&appliedMigration{}).Order("applied_at, id").Pluck("id", &ids).Error; err != nil {
				t.Fatal(err)
			}
			if len(ids) != len(tt.wantApply) {
				t.Fatalf("applied = %v, want %v", ids, tt.wantApply)
			}
			for i := range ids {
				if ids[i] != tt.wantApply[i] {
					t.Errorf("applied = %v, want %v", ids, tt.wantApply)
					break
				}
			}
		})
	}
}

func TestMigrationFailureFromOpen(t *testing.T) {
	// A closed database makes the first migration query fail.
	s := closedStore(t)
	assertRoute(t, s.migrate(context.Background()), "migrate->Store.runMigrations.MigrationFailed")
}
