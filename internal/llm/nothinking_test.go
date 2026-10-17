package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNoThinkingFieldsReachTheServer(t *testing.T) {
	const (
		openAIBody = `{"id":"1","object":"chat.completion","model":"m","choices":[{"index":0,` +
			`"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		anthropicBody = `{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		ollamaBody = `{"model":"m","message":{"role":"assistant","content":"ok"},"done":true,"prompt_eval_count":1,"eval_count":1}`
	)
	tests := []struct {
		name       string
		provider   Provider
		reply      string
		noThinking bool
		want       map[string]string // field -> JSON, as the server must receive it
		absent     []string
	}{
		{"ollama off", ProviderOllama, ollamaBody, false, nil, []string{"think"}},
		{"ollama on", ProviderOllama, ollamaBody, true, map[string]string{"think": `false`}, nil},
		{"openai-compatible off", ProviderOpenAICompatible, openAIBody, false, nil, []string{"thinking", "enable_thinking", "chat_template_kwargs"}},
		{"openai-compatible on", ProviderOpenAICompatible, openAIBody, true, map[string]string{
			"thinking":             `{"type":"disabled"}`,
			"enable_thinking":      `false`,
			"chat_template_kwargs": `{"enable_thinking":false}`,
		}, nil},
		{"anthropic has nothing to send", ProviderAnthropic, anthropicBody, true, nil, []string{"think", "thinking", "enable_thinking"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var body map[string]json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read body: %v", err)
				}
				mu.Lock()
				body = nil
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Errorf("the server received a body that is not JSON: %v\n%s", err, raw)
				}
				mu.Unlock()
				if int64(len(raw)) != r.ContentLength {
					t.Errorf("Content-Length = %d, body has %d bytes", r.ContentLength, len(raw))
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tt.reply)); err != nil {
					t.Errorf("write: %v", err)
				}
			}))
			defer srv.Close()

			model, err := New(ModelConfig{Provider: tt.provider, Model: "m", BaseURL: srv.URL, APIKey: "k", Temperature: 0.2, NoThinking: tt.noThinking})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Generate(context.Background(), model, Request{System: "be brief", Prompt: "hi", Temperature: 0.2})
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != "ok" {
				t.Errorf("text = %q", got.Text)
			}
			mu.Lock()
			defer mu.Unlock()
			if _, ok := body["model"]; !ok {
				t.Errorf("the request lost its own fields: %v", body)
			}
			for k, v := range tt.want {
				if string(body[k]) != v {
					t.Errorf("%s = %s, want %s", k, body[k], v)
				}
			}
			for _, k := range tt.absent {
				if _, ok := body[k]; ok {
					t.Errorf("%s must not be sent", k)
				}
			}
		})
	}
}

func TestFieldInjector(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // JSON the server must receive; "" means unchanged
	}{
		{"adds a missing field", `{"a":1}`, `{"a":1,"x":true}`},
		{"keeps a field that is set", `{"x":false}`, `{"x":false}`},
		{"leaves a body that is not an object", `[1,2]`, ""},
		{"leaves text", `not json`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, _ = io.ReadAll(r.Body)
			}))
			defer srv.Close()
			client := &http.Client{Transport: &fieldInjector{base: http.DefaultTransport, fields: map[string]json.RawMessage{"x": json.RawMessage(`true`)}}}
			req, err := http.NewRequest(http.MethodPost, srv.URL, bytesReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			want := tt.want
			if want == "" {
				want = tt.body
			}
			if tt.want != "" {
				var a, b map[string]any
				if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil || len(a) != len(b) {
					t.Errorf("server got %s, want %s", got, want)
				}
				for k, v := range b {
					if a[k] != v {
						t.Errorf("server got %s, want %s", got, want)
					}
				}
			} else if string(got) != want {
				t.Errorf("server got %q, want it unchanged: %q", got, want)
			}
		})
	}
}

func TestSupportsNoThinking(t *testing.T) {
	for p, want := range map[Provider]bool{ProviderOllama: true, ProviderOpenAICompatible: true, ProviderAnthropic: false} {
		if got := SupportsNoThinking(p); got != want {
			t.Errorf("SupportsNoThinking(%s) = %v, want %v", p, got, want)
		}
	}
}

func bytesReader(s string) io.Reader { return strings.NewReader(s) }
