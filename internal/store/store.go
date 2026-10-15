// Package store keeps LLM configurations, saved documents and settings in
// SQLite through gorm. The schema is created by numbered migrations.
package store

import (
	"context"
	"net/url"
	"os"

	"github.com/rm4n0s/errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/rm4n0s/english-tutor/internal/config"
)

// Store wraps an open, migrated database. There is no package-level database
// variable, so tests build their own Store with OpenMemory.
type Store struct {
	db *gorm.DB
}

// Open opens the database file at path, creating it with mode 0600 if needed
// (and tightening an existing file to 0600), then runs the migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, config.FileMode)
	if err != nil {
		return nil, errors.NewErr("DatabaseOpenFailed", err).SetMetadata("path", path)
	}
	if err := f.Chmod(config.FileMode); err != nil {
		_ = f.Close()
		return nil, errors.NewErr("DatabaseOpenFailed", err).SetMetadata("path", path)
	}
	if err := f.Close(); err != nil {
		return nil, errors.NewErr("DatabaseOpenFailed", err).SetMetadata("path", path)
	}
	dsn := (&url.URL{Scheme: "file", Opaque: (&url.URL{Path: path}).EscapedPath(), RawQuery: "_busy_timeout=5000"}).String()
	return openDSN(ctx, dsn, false)
}

// OpenMemory opens a private in-memory database. It is meant for tests.
func OpenMemory(ctx context.Context) (*Store, error) {
	return openDSN(ctx, ":memory:", true)
}

func openDSN(ctx context.Context, dsn string, single bool) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:         logger.Discard,
		TranslateError: true,
	})
	if err != nil {
		return nil, errors.NewErr("DatabaseOpenFailed", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, errors.NewErr("DatabaseOpenFailed", err)
	}
	if single {
		// Every connection to ":memory:" is its own database.
		sqlDB.SetMaxOpenConns(1)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, errors.NewErr("DatabaseOpenFailed", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return errors.NewErr("DatabaseQueryFailed", err)
	}
	if err := sqlDB.Close(); err != nil {
		return errors.NewErr("DatabaseQueryFailed", err)
	}
	return nil
}
