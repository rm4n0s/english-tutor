package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestUsageFromRealClients runs the real langchaingo clients against local
// servers that speak each provider's wire format. It guards the GenerationInfo
// key names that UsageFromInfo depends on, and the explicit credentials.
func TestUsageFromRealClients(t *testing.T) {
	const (
		openAIBody = `{"id":"1","object":"chat.completion","model":"m","choices":[{"index":0,` +
			`"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`
		anthropicBody = `{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"hi there"}],"stop_reason":"end_turn",` +
			`"usage":{"input_tokens":11,"output_tokens":5}}`
		ollamaBody = `{"model":"m","message":{"role":"assistant","content":"hi there"},` +
			`"done":true,"prompt_eval_count":11,"eval_count":5}`
	)
	tests := []struct {
		name       string
		provider   Provider
		body       string
		apiKey     string
		wantAuth   string // substring expected in the Authorization or x-api-key header
		authHeader string
	}{
		{"openai-compatible", ProviderOpenAICompatible, openAIBody, "sk-test", "sk-test", "Authorization"},
		{"openai-compatible without key", ProviderOpenAICompatible, openAIBody, "", noKey, "Authorization"},
		{"anthropic", ProviderAnthropic, anthropicBody, "sk-ant", "sk-ant", "X-Api-Key"},
		{"ollama", ProviderOllama, ollamaBody, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Environment credentials must lose against the configured ones.
			t.Setenv("OPENAI_API_KEY", "from-env")
			t.Setenv("ANTHROPIC_API_KEY", "from-env")

			var mu sync.Mutex
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotAuth = r.Header.Get(tt.authHeader)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("write: %v", err)
				}
			}))
			defer srv.Close()

			model, err := New(ModelConfig{Provider: tt.provider, Model: "m", BaseURL: srv.URL, APIKey: tt.apiKey, Temperature: 0.2})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Generate(context.Background(), model, Request{System: "be brief", Prompt: "hi", Temperature: 0.2})
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != "hi there" {
				t.Errorf("text = %q", got.Text)
			}
			if want := (Usage{InputTokens: 11, OutputTokens: 5}); got.Usage != want {
				t.Errorf("usage = %+v, want %+v (provider counts, not estimated)", got.Usage, want)
			}
			mu.Lock()
			defer mu.Unlock()
			if tt.wantAuth != "" && !strings.Contains(gotAuth, tt.wantAuth) {
				t.Errorf("%s header = %q, want it to contain %q", tt.authHeader, gotAuth, tt.wantAuth)
			}
			if strings.Contains(gotAuth, "from-env") {
				t.Errorf("%s header used an environment key: %q", tt.authHeader, gotAuth)
			}
		})
	}
}
