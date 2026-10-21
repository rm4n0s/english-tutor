// Package llm builds langchaingo models from the user's configuration and
// normalises token usage. It does not import a GUI toolkit.
package llm

import (
	"net/url"
	"strings"

	"github.com/rm4n0s/errors"
)

// Provider names a kind of LLM server.
type Provider string

// The supported providers. The string values are what is stored in the
// database.
const (
	ProviderOllama           Provider = "ollama"
	ProviderOpenAICompatible Provider = "openai-compatible"
	ProviderAnthropic        Provider = "anthropic"
)

// DefaultProvider is the provider a new configuration starts with in the
// Preferences forms.
const DefaultProvider = ProviderOpenAICompatible

// Providers lists every supported provider in the order shown to the user.
func Providers() []Provider {
	return []Provider{ProviderOllama, ProviderOpenAICompatible, ProviderAnthropic}
}

// ParseProvider converts a stored string into a Provider.
func ParseProvider(s string) (Provider, error) {
	for _, p := range Providers() {
		if string(p) == s {
			return p, nil
		}
	}
	return "", errors.New("UnknownProvider", "Provider must be ollama, openai-compatible or anthropic.", "provider", s)
}

// ModelConfig is everything needed to build a model. APIKey is a secret: it
// must never be logged or put in error metadata.
type ModelConfig struct {
	Provider    Provider
	Model       string
	BaseURL     string
	APIKey      string
	Temperature float64
	// NoThinking asks the model not to think before it answers. It does
	// something only for the providers in SupportsNoThinking.
	NoThinking bool
}

// Validate checks that the fields the provider needs are present. A missing
// field gives LLMConfigInvalid, an unsupported provider gives UnknownProvider.
// The message of either is meant to be shown to the user.
func (c ModelConfig) Validate() error {
	if _, err := ParseProvider(string(c.Provider)); err != nil {
		return err
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
	}
	if c.BaseURL != "" {
		// langchaingo's ollama.WithServerURL calls log.Fatal on a URL that
		// does not parse, so a bad URL must never reach it.
		u, err := url.Parse(strings.TrimSpace(c.BaseURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalidConfig("base_url", "Base URL must be an address such as http://localhost:11434.")
		}
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		return invalidConfig("temperature", "Temperature must be between 0 and 2.")
	}
	return nil
}

func invalidConfig(field, message string) error {
	return errors.New("LLMConfigInvalid", message, "field", field)
}
