package llm

import (
	"testing"

	"github.com/rm4n0s/errors"
)

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with route %q, got nil", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestParseProvider(t *testing.T) {
	tests := []struct {
		in      string
		want    Provider
		wantErr bool
	}{
		{in: "ollama", want: ProviderOllama},
		{in: "openai-compatible", want: ProviderOpenAICompatible},
		{in: "anthropic", want: ProviderAnthropic},
		{in: "", wantErr: true},
		{in: "Ollama", wantErr: true},
		{in: "bard", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseProvider(tt.in)
			if tt.wantErr {
				assertRoute(t, err, "ParseProvider.UnknownProvider")
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ParseProvider(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func validOllama() ModelConfig {
	return ModelConfig{Provider: ProviderOllama, Model: "llama3.1", BaseURL: "http://localhost:11434", Temperature: 0.2}
}

func TestModelConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *ModelConfig)
		wantField string // empty means valid
		unknown   bool
	}{
		{name: "valid ollama", mutate: func(*ModelConfig) {}},
		{name: "empty provider", mutate: func(c *ModelConfig) { c.Provider = "" }, unknown: true},
		{name: "unknown provider", mutate: func(c *ModelConfig) { c.Provider = "bard" }, unknown: true},
		{name: "missing model", mutate: func(c *ModelConfig) { c.Model = " " }, wantField: "model"},
		{name: "ollama without url", mutate: func(c *ModelConfig) { c.BaseURL = "" }, wantField: "base_url"},
		{name: "url without scheme", mutate: func(c *ModelConfig) { c.BaseURL = "localhost:11434" }, wantField: "base_url"},
		{name: "url with other scheme", mutate: func(c *ModelConfig) { c.BaseURL = "ftp://host" }, wantField: "base_url"},
		{name: "url without host", mutate: func(c *ModelConfig) { c.BaseURL = "http://" }, wantField: "base_url"},
		{name: "url that does not parse", mutate: func(c *ModelConfig) { c.BaseURL = "http://a b/%zz" }, wantField: "base_url"},
		{name: "https url", mutate: func(c *ModelConfig) { c.BaseURL = "https://example.com/v1" }},
		{
			name:   "openai-compatible with url and no key",
			mutate: func(c *ModelConfig) { c.Provider = ProviderOpenAICompatible },
		},
		{
			name:      "openai-compatible without url",
			mutate:    func(c *ModelConfig) { c.Provider = ProviderOpenAICompatible; c.BaseURL = "" },
			wantField: "base_url",
		},
		{
			name:   "anthropic with key, no url",
			mutate: func(c *ModelConfig) { c.Provider = ProviderAnthropic; c.BaseURL = ""; c.APIKey = "k" },
		},
		{
			name:      "anthropic without key",
			mutate:    func(c *ModelConfig) { c.Provider = ProviderAnthropic; c.BaseURL = "" },
			wantField: "api_key",
		},
		{
			name:      "anthropic with bad url",
			mutate:    func(c *ModelConfig) { c.Provider = ProviderAnthropic; c.APIKey = "k"; c.BaseURL = "nope" },
			wantField: "base_url",
		},
		{name: "temperature 0", mutate: func(c *ModelConfig) { c.Temperature = 0 }},
		{name: "temperature 2", mutate: func(c *ModelConfig) { c.Temperature = 2 }},
		{name: "temperature above 2", mutate: func(c *ModelConfig) { c.Temperature = 2.1 }, wantField: "temperature"},
		{name: "temperature below 0", mutate: func(c *ModelConfig) { c.Temperature = -1 }, wantField: "temperature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validOllama()
			tt.mutate(&cfg)
			err := cfg.Validate()
			switch {
			case tt.unknown:
				assertRoute(t, err, "ModelConfig.Validate->ParseProvider.UnknownProvider")
			case tt.wantField != "":
				assertRoute(t, err, "ModelConfig.Validate->invalidConfig.LLMConfigInvalid")
				appErr, _ := errors.FromError(err)
				if got := appErr.Metadata["field"]; got != tt.wantField {
					t.Errorf("field = %v, want %s", got, tt.wantField)
				}
			default:
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
			}
		})
	}
}

func TestValidateErrorsDoNotLeakKey(t *testing.T) {
	cfg := ModelConfig{Provider: ProviderAnthropic, Model: " ", APIKey: "sk-secret"}
	err := cfg.Validate()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	for k, v := range appErr.Metadata {
		if v == "sk-secret" {
			t.Errorf("metadata %q leaks the API key", k)
		}
	}
}
