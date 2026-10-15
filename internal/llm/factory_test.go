package llm

import (
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"
)

func TestNewBuildsEveryProvider(t *testing.T) {
	tests := []struct {
		name string
		cfg  ModelConfig
	}{
		{"ollama", validOllama()},
		{"openai-compatible with key", ModelConfig{Provider: ProviderOpenAICompatible, Model: "gpt-4o", BaseURL: "https://api.example.com/v1", APIKey: "k"}},
		{"openai-compatible without key", ModelConfig{Provider: ProviderOpenAICompatible, Model: "local", BaseURL: "http://localhost:1234/v1"}},
		{"anthropic default url", ModelConfig{Provider: ProviderAnthropic, Model: "claude-sonnet-5-5", APIKey: "k"}},
		{"anthropic custom url", ModelConfig{Provider: ProviderAnthropic, Model: "claude-sonnet-5-5", APIKey: "k", BaseURL: "https://proxy.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New(tt.cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if m == nil {
				t.Fatal("New() returned a nil model")
			}
		})
	}
}

func TestNewIgnoresEnvironment(t *testing.T) {
	// The environment must neither supply a missing key nor change the model.
	t.Setenv("OPENAI_API_KEY", "from-env")
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Setenv("OLLAMA_HOST", "http://elsewhere:1")
	_, err := New(ModelConfig{Provider: ProviderAnthropic, Model: "m"})
	assertRoute(t, err, "New->newWith->ModelConfig.Validate->invalidConfig.LLMConfigInvalid")
}

func TestNewFailures(t *testing.T) {
	tests := []struct {
		name  string
		cfg   ModelConfig
		route string
	}{
		{"unknown provider", ModelConfig{Provider: "bard", Model: "m", BaseURL: "http://x"}, "newWith->ModelConfig.Validate->ParseProvider.UnknownProvider"},
		{"invalid config", ModelConfig{Provider: ProviderOllama, Model: "", BaseURL: "http://x"}, "newWith->ModelConfig.Validate->invalidConfig.LLMConfigInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			assertRoute(t, err, tt.route)
		})
	}
}

func TestNewWithBuilderFailures(t *testing.T) {
	boom := errors.New("Inner", "constructor failed")
	cfg := ModelConfig{Provider: ProviderAnthropic, Model: "m", APIKey: "sk-secret"}

	t.Run("constructor error", func(t *testing.T) {
		_, err := newWith(cfg, map[Provider]builder{
			ProviderAnthropic: func(ModelConfig) (llms.Model, error) { return nil, boom },
		})
		assertRoute(t, err, "newWith->clientCreateFailed.LLMClientCreateFailed")
		appErr, _ := errors.FromError(err)
		if !errors.Is(appErr.OriginalErr, boom) {
			t.Errorf("OriginalErr = %v, want the constructor error", appErr.OriginalErr)
		}
		for k, v := range appErr.Metadata {
			if v == "sk-secret" {
				t.Errorf("metadata %q leaks the API key", k)
			}
		}
	})

	t.Run("no builder for a valid provider", func(t *testing.T) {
		_, err := newWith(cfg, map[Provider]builder{})
		assertRoute(t, err, "newWith.UnknownProvider")
	})
}
