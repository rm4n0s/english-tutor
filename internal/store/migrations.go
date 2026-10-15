package store

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// A migration creates or changes the schema. Migrations are applied in order,
// each in its own transaction, and recorded in schema_migrations by ID.
//
// Migrations use their own frozen copies of the table structs (see
// migration001Init) so that a later change to a model cannot rewrite history.
type migration struct {
	id string
	up func(tx *gorm.DB) error
}

type appliedMigration struct {
	ID        string `gorm:"primaryKey"`
	AppliedAt time.Time
}

func (appliedMigration) TableName() string { return "schema_migrations" }

// migrations lists every migration in the order it must run. Add new ones at
// the end; never edit or reorder an existing entry.
func migrations() []migration {
	return []migration{
		{id: "001_init", up: migration001Init},
		{id: "002_error_events", up: migration002ErrorEvents},
		{id: "003_llm_usage", up: migration003LLMUsage},
	}
}

func (s *Store) migrate(ctx context.Context) error {
	return s.runMigrations(ctx, migrations())
}

func (s *Store) runMigrations(ctx context.Context, list []migration) error {
	db := s.db.WithContext(ctx)
	if !db.Migrator().HasTable(&appliedMigration{}) {
		if err := db.Migrator().CreateTable(&appliedMigration{}); err != nil {
			return errors.NewErr("MigrationFailed", err).SetMetadata("migration", "schema_migrations")
		}
	}
	for _, m := range list {
		var count int64
		if err := db.Model(&appliedMigration{}).Where(&appliedMigration{ID: m.id}).Count(&count).Error; err != nil {
			return errors.NewErr("MigrationFailed", err).SetMetadata("migration", m.id)
		}
		if count > 0 {
			continue
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := m.up(tx); err != nil {
				return err
			}
			return tx.Create(&appliedMigration{ID: m.id, AppliedAt: time.Now()}).Error
		})
		if err != nil {
			return errors.NewErr("MigrationFailed", err).SetMetadata("migration", m.id)
		}
	}
	return nil
}

type llmConfigV1 struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"not null;uniqueIndex"`
	Provider    string `gorm:"not null"`
	Model       string `gorm:"not null"`
	BaseURL     string
	APIKey      string
	Temperature float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (llmConfigV1) TableName() string { return "llm_configs" }

type documentV1 struct {
	ID        uint   `gorm:"primaryKey"`
	Title     string `gorm:"not null"`
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (documentV1) TableName() string { return "documents" }

type settingV1 struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

func (settingV1) TableName() string { return "settings" }

func migration001Init(tx *gorm.DB) error {
	return tx.Migrator().CreateTable(&llmConfigV1{}, &documentV1{}, &settingV1{})
}

type errorEventV2 struct {
	ID          uint   `gorm:"primaryKey"`
	ErrorType   string `gorm:"not null;index:idx_error_events_type_created,priority:1"`
	Category    string `gorm:"not null"`
	Snippet     string
	SourceKey   string `gorm:"not null;uniqueIndex:ux_error_events_source_fingerprint,priority:1"`
	Fingerprint string `gorm:"not null;uniqueIndex:ux_error_events_source_fingerprint,priority:2"`
	LLMName     string
	CreatedAt   time.Time `gorm:"not null;index:idx_error_events_type_created,priority:2"`
}

func (errorEventV2) TableName() string { return "error_events" }

func migration002ErrorEvents(tx *gorm.DB) error {
	return tx.Migrator().CreateTable(&errorEventV2{})
}

type llmUsageV3 struct {
	ID           uint      `gorm:"primaryKey"`
	LLMName      string    `gorm:"not null"`
	Kind         string    `gorm:"not null"`
	InputTokens  int       `gorm:"not null"`
	OutputTokens int       `gorm:"not null"`
	Estimated    bool      `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;index"`
}

func (llmUsageV3) TableName() string { return "llm_usage" }

func migration003LLMUsage(tx *gorm.DB) error {
	return tx.Migrator().CreateTable(&llmUsageV3{})
}
