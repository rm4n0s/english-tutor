package store

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

func TestLLMConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *LLMConfig)
		wantField string // empty means valid
	}{
		{name: "valid ollama", mutate: func(*LLMConfig) {}},
		{name: "empty name", mutate: func(c *LLMConfig) { c.Name = "  " }, wantField: "name"},
		{name: "empty model", mutate: func(c *LLMConfig) { c.Model = "" }, wantField: "model"},
		{name: "bad base url", mutate: func(c *LLMConfig) { c.BaseURL = "localhost:11434/\x7f" }, wantField: "base_url"},
		{name: "base url without scheme", mutate: func(c *LLMConfig) { c.BaseURL = "localhost:11434" }, wantField: "base_url"},
		{name: "ollama needs base url", mutate: func(c *LLMConfig) { c.BaseURL = "" }, wantField: "base_url"},
		{
			name:   "openai-compatible without key is allowed",
			mutate: func(c *LLMConfig) { c.Provider = string(llm.ProviderOpenAICompatible); c.APIKey = "" },
		},
		{
			name:      "openai-compatible needs base url",
			mutate:    func(c *LLMConfig) { c.Provider = string(llm.ProviderOpenAICompatible); c.BaseURL = "" },
			wantField: "base_url",
		},
		{
			name:   "anthropic with key and no base url",
			mutate: func(c *LLMConfig) { c.Provider = string(llm.ProviderAnthropic); c.BaseURL = ""; c.APIKey = "k" },
		},
		{
			name:      "anthropic needs key",
			mutate:    func(c *LLMConfig) { c.Provider = string(llm.ProviderAnthropic); c.BaseURL = "" },
			wantField: "api_key",
		},
		{name: "temperature too high", mutate: func(c *LLMConfig) { c.Temperature = 2.5 }, wantField: "temperature"},
		{name: "temperature negative", mutate: func(c *LLMConfig) { c.Temperature = -0.1 }, wantField: "temperature"},
		{name: "temperature zero is valid", mutate: func(c *LLMConfig) { c.Temperature = 0 }},
		{name: "default timeout is valid", mutate: func(c *LLMConfig) { c.TimeoutSeconds = 0 }},
		{name: "timeout in range is valid", mutate: func(c *LLMConfig) { c.TimeoutSeconds = MaxTimeoutSeconds }},
		{name: "timeout too short", mutate: func(c *LLMConfig) { c.TimeoutSeconds = MinTimeoutSeconds - 1 }, wantField: "timeout"},
		{name: "timeout negative", mutate: func(c *LLMConfig) { c.TimeoutSeconds = -1 }, wantField: "timeout"},
		{name: "timeout too long", mutate: func(c *LLMConfig) { c.TimeoutSeconds = MaxTimeoutSeconds + 1 }, wantField: "timeout"},
		{name: "parallel limit is valid", mutate: func(c *LLMConfig) { c.MaxParallel = MaxParallelLimit }},
		{name: "parallel negative", mutate: func(c *LLMConfig) { c.MaxParallel = -1 }, wantField: "max_parallel"},
		{name: "parallel too many", mutate: func(c *LLMConfig) { c.MaxParallel = MaxParallelLimit + 1 }, wantField: "max_parallel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig("x")
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			assertRoute(t, err, "LLMConfig.Validate")
			assertRoute(t, err, "LLMConfigInvalid")
			if got := errMetadata(t, err, "field"); got != tt.wantField {
				t.Errorf("field = %v, want %s", got, tt.wantField)
			}
		})
	}
}

func TestLLMConfigUnknownProvider(t *testing.T) {
	cfg := validConfig("x")
	cfg.Provider = "bard"
	assertRoute(t, cfg.Validate(), "LLMConfig.Validate->ModelConfig.Validate->ParseProvider.UnknownProvider")
}

func TestLLMConfigAPIKeyNotInError(t *testing.T) {
	cfg := validConfig("x")
	cfg.Provider = string(llm.ProviderAnthropic)
	cfg.APIKey = "sk-secret"
	cfg.Model = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	appErr := mustAppErr(t, err)
	for k, v := range appErr.Metadata {
		if v == "sk-secret" {
			t.Errorf("metadata %q leaks the API key", k)
		}
	}
	if appErr.Message == "sk-secret" {
		t.Error("message leaks the API key")
	}
}

func TestLLMConfigCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	b := validConfig("beta")
	a := validConfig("alpha")
	for _, c := range []*LLMConfig{b, a} {
		if err := s.CreateLLMConfig(ctx, c); err != nil {
			t.Fatal(err)
		}
		if c.ID == 0 {
			t.Fatal("ID not set")
		}
	}

	list, err := s.ListLLMConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "beta" {
		t.Fatalf("ListLLMConfigs() = %+v, want alpha then beta", list)
	}

	got, err := s.GetLLMConfig(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "beta" || got.Model != "llama3.1" || got.Temperature != DefaultTemperature {
		t.Errorf("GetLLMConfig() = %+v", got)
	}

	createdAt := got.CreatedAt
	got.Name = "gamma"
	got.Provider = string(llm.ProviderAnthropic)
	got.APIKey = "sk-1"
	got.BaseURL = ""
	got.Model = "claude-sonnet-5-5"
	got.Temperature = 0.7
	got.TimeoutSeconds = 240
	got.MaxParallel = 4
	if err := s.UpdateLLMConfig(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, err := s.GetLLMConfig(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "gamma" || again.Provider != string(llm.ProviderAnthropic) || again.APIKey != "sk-1" ||
		again.BaseURL != "" || again.Temperature != 0.7 || again.TimeoutSeconds != 240 || again.MaxParallel != 4 {
		t.Errorf("after update = %+v", again)
	}
	if !again.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt changed from %v to %v", createdAt, again.CreatedAt)
	}

	if err := s.DeleteLLMConfig(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListLLMConfigs(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "alpha" {
		t.Errorf("after delete ListLLMConfigs() = %+v, %v", list, err)
	}
}

func TestLLMConfigFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		run   func(s *Store) error
		route string
	}{
		{
			name: "create with invalid config",
			run: func(s *Store) error {
				c := validConfig("x")
				c.Model = ""
				return s.CreateLLMConfig(ctx, c)
			},
			route: "Store.CreateLLMConfig->LLMConfig.Validate->ModelConfig.Validate->invalidConfig.LLMConfigInvalid",
		},
		{
			name: "create with duplicate name",
			run: func(s *Store) error {
				if err := s.CreateLLMConfig(ctx, validConfig("dup")); err != nil {
					return err
				}
				return s.CreateLLMConfig(ctx, validConfig("dup"))
			},
			route: "Store.CreateLLMConfig.LLMConfigNameTaken",
		},
		{
			name: "update to a name that is taken",
			run: func(s *Store) error {
				one, other := validConfig("one"), validConfig("other")
				if err := s.CreateLLMConfig(ctx, one); err != nil {
					return err
				}
				if err := s.CreateLLMConfig(ctx, other); err != nil {
					return err
				}
				other.Name = "one"
				return s.UpdateLLMConfig(ctx, other)
			},
			route: "Store.UpdateLLMConfig.LLMConfigNameTaken",
		},
		{
			name: "update with invalid config",
			run: func(s *Store) error {
				c := validConfig("x")
				c.Provider = "nope"
				return s.UpdateLLMConfig(ctx, c)
			},
			route: "Store.UpdateLLMConfig->LLMConfig.Validate->ModelConfig.Validate->ParseProvider.UnknownProvider",
		},
		{
			name: "update missing config",
			run: func(s *Store) error {
				c := validConfig("x")
				c.ID = 99
				return s.UpdateLLMConfig(ctx, c)
			},
			route: "Store.UpdateLLMConfig.LLMConfigNotFound",
		},
		{
			name: "get missing config",
			run: func(s *Store) error {
				_, err := s.GetLLMConfig(ctx, 99)
				return err
			},
			route: "Store.GetLLMConfig.LLMConfigNotFound",
		},
		{
			name:  "delete missing config",
			run:   func(s *Store) error { return s.DeleteLLMConfig(ctx, 99) },
			route: "Store.DeleteLLMConfig.LLMConfigNotFound",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRoute(t, tt.run(newTestStore(t)), tt.route)
		})
	}
}

func TestLLMConfigQueryFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		run   func(s *Store) error
		route string
	}{
		{"create", func(s *Store) error { return s.CreateLLMConfig(ctx, validConfig("x")) }, "Store.CreateLLMConfig.DatabaseQueryFailed"},
		{"update", func(s *Store) error { c := validConfig("x"); c.ID = 1; return s.UpdateLLMConfig(ctx, c) }, "Store.UpdateLLMConfig.DatabaseQueryFailed"},
		{"get", func(s *Store) error { _, err := s.GetLLMConfig(ctx, 1); return err }, "Store.GetLLMConfig.DatabaseQueryFailed"},
		{"list", func(s *Store) error { _, err := s.ListLLMConfigs(ctx); return err }, "Store.ListLLMConfigs.DatabaseQueryFailed"},
		{"delete", func(s *Store) error { return s.DeleteLLMConfig(ctx, 1) }, "Store.DeleteLLMConfig.DatabaseQueryFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRoute(t, tt.run(closedStore(t)), tt.route)
		})
	}
}

func TestLLMConfigTimeout(t *testing.T) {
	cfg := validConfig("x")
	if cfg.Timeout() != 0 {
		t.Errorf("Timeout() = %v, want 0 for the default", cfg.Timeout())
	}
	cfg.TimeoutSeconds = 90
	if cfg.Timeout() != 90*time.Second {
		t.Errorf("Timeout() = %v, want 90s", cfg.Timeout())
	}
}

func TestLLMRequestSettingsMigration(t *testing.T) {
	// A configuration saved before the settings existed reads as the defaults.
	ctx := context.Background()
	s := newTestStore(t)
	old := map[string]any{"name": "old", "provider": "ollama", "model": "llama3.1", "base_url": "http://localhost:11434", "temperature": 0.2}
	if err := s.db.Table("llm_configs").Create(old).Error; err != nil {
		t.Fatal(err)
	}
	list, err := s.ListLLMConfigs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	if list[0].TimeoutSeconds != 0 || list[0].MaxParallel != 0 {
		t.Errorf("old row = %+v, want timeout 0 and parallel 0", list[0])
	}
}
