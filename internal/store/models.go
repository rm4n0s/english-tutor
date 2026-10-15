package store

import (
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// Provider names stored in LLMConfig.Provider.
const (
	ProviderOllama           = "ollama"
	ProviderOpenAICompatible = "openai-compatible"
	ProviderAnthropic        = "anthropic"
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

// Validate checks that the fields required by the provider are present. The
// message of the returned error is meant to be shown to the user.
func (c *LLMConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return invalidConfig("name", "Name is required.")
	}
	if strings.TrimSpace(c.Model) == "" {
		return invalidConfig("model", "Model is required.")
	}
	switch c.Provider {
	case ProviderOllama, ProviderOpenAICompatible:
		if strings.TrimSpace(c.BaseURL) == "" {
			return invalidConfig("base_url", "Base URL is required for this provider.")
		}
	case ProviderAnthropic:
		if strings.TrimSpace(c.APIKey) == "" {
			return invalidConfig("api_key", "API key is required for Anthropic.")
		}
	default:
		return invalidConfig("provider", "Provider must be ollama, openai-compatible or anthropic.")
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		return invalidConfig("temperature", "Temperature must be between 0 and 2.")
	}
	return nil
}

func invalidConfig(field, message string) error {
	return errors.New("LLMConfigInvalid", message, "field", field)
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
