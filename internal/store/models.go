package store

import (
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// DefaultTemperature is used when the user does not choose one.
const DefaultTemperature = 0.2

// LLMConfig is one LLM the user can select. APIKey is stored in plain text in
// the database file (mode 0600) and must never be logged or put in error
// metadata.
type LLMConfig struct {
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

// ModelConfig converts c to the form the llm package builds models from.
func (c *LLMConfig) ModelConfig() llm.ModelConfig {
	return llm.ModelConfig{
		Provider:    llm.Provider(c.Provider),
		Model:       c.Model,
		BaseURL:     strings.TrimSpace(c.BaseURL),
		APIKey:      c.APIKey,
		Temperature: c.Temperature,
	}
}

// Validate checks the name and the fields the provider needs. The message of
// the returned error is meant to be shown to the user.
func (c *LLMConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("LLMConfigInvalid", "Name is required.", "field", "name")
	}
	return c.ModelConfig().Validate()
}

// Document is a text saved in the library.
type Document struct {
	ID        uint   `gorm:"primaryKey"`
	Title     string `gorm:"not null"`
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Setting is one key/value pair in the settings table.
type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// ErrorEvent is one recorded mistake (section 5.7). The pair (SourceKey,
// Fingerprint) is unique.
type ErrorEvent struct {
	ID          uint   `gorm:"primaryKey"`
	ErrorType   string `gorm:"not null;index:idx_error_events_type_created,priority:1"`
	Category    string `gorm:"not null"`
	Snippet     string
	SourceKey   string `gorm:"not null;uniqueIndex:ux_error_events_source_fingerprint,priority:1"`
	Fingerprint string `gorm:"not null;uniqueIndex:ux_error_events_source_fingerprint,priority:2"`
	LLMName     string
	CreatedAt   time.Time `gorm:"not null;index:idx_error_events_type_created,priority:2"`
}

// LLMUsage is the token usage of one request (section 5.8).
type LLMUsage struct {
	ID           uint      `gorm:"primaryKey"`
	LLMName      string    `gorm:"not null"`
	Kind         string    `gorm:"not null"`
	InputTokens  int       `gorm:"not null"`
	OutputTokens int       `gorm:"not null"`
	Estimated    bool      `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;index"`
}

func (LLMUsage) TableName() string { return "llm_usage" }
