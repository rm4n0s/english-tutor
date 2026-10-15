package analysis

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// hang answers only when the client gives up. The body is read first because the
// server notices a closed connection only after the request has been read.
func hang(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-time.After(5 * time.Second):
	case <-r.Context().Done():
	}
}

func ollamaServer(t *testing.T, handler http.HandlerFunc) llm.ModelConfig {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return llm.ModelConfig{Provider: llm.ProviderOllama, Model: "m", BaseURL: srv.URL, Temperature: 0.2}
}

const ollamaOK = `{"model":"m","message":{"role":"assistant","content":"ok"},"done":true,"prompt_eval_count":3,"eval_count":1}`

func TestTestConnectionSucceeds(t *testing.T) {
	var gotPrompt string
	cfg := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read: %v", err)
		}
		gotPrompt = string(body)
		if _, err := w.Write([]byte(ollamaOK)); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	res, err := TestConnection(context.Background(), "local", cfg, 0)
	if err != nil || res.Cancelled || res.Elapsed <= 0 {
		t.Fatalf("TestConnection() = %+v, %v", res, err)
	}
	if !strings.Contains(gotPrompt, "single word") {
		t.Errorf("request did not carry the test prompt: %s", gotPrompt)
	}
}

func TestTestConnectionFailures(t *testing.T) {
	tests := []struct {
		name      string
		cfg       func(t *testing.T) llm.ModelConfig
		timeout   time.Duration
		route     string
		wantInMsg string
	}{
		{
			name: "server error",
			cfg: func(t *testing.T) llm.ModelConfig {
				return ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "model not found", http.StatusNotFound)
				})
			},
			route:     "classifyError.LLMCallFailed",
			wantInMsg: "local",
		},
		{
			name: "slow server",
			cfg: func(t *testing.T) llm.ModelConfig {
				return ollamaServer(t, hang)
			},
			timeout:   50 * time.Millisecond,
			route:     "classifyError.LLMCallTimeout",
			wantInMsg: "took longer",
		},
		{
			name:  "invalid configuration",
			cfg:   func(*testing.T) llm.ModelConfig { return llm.ModelConfig{Provider: llm.ProviderOllama, Model: "m"} },
			route: "New->newWith->ModelConfig.Validate->invalidConfig.LLMConfigInvalid", wantInMsg: "Base URL",
		},
		{
			name: "unknown provider",
			cfg: func(*testing.T) llm.ModelConfig {
				return llm.ModelConfig{Provider: "bard", Model: "m", BaseURL: "http://x"}
			},
			route: "New->newWith->ModelConfig.Validate->ParseProvider.UnknownProvider", wantInMsg: "Provider",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := TestConnection(context.Background(), "local", tt.cfg(t), tt.timeout)
			assertRoute(t, err, "TestConnection->"+tt.route)
			if !strings.Contains(err.Error(), tt.wantInMsg) {
				t.Errorf("message %q should contain %q", err.Error(), tt.wantInMsg)
			}
		})
	}
}

func TestTestConnectionCancelled(t *testing.T) {
	cfg := ollamaServer(t, hang)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	res, err := TestConnection(ctx, "local", cfg, 0)
	if err != nil || !res.Cancelled {
		t.Errorf("TestConnection() = %+v, %v; want cancelled and no error", res, err)
	}
}
