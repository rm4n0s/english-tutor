package llm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// noThinkingFields are the fields added to every request body of a provider
// when "No thinking" is on. None of them is a standard of the chat protocols:
// each model family has its own way to switch thinking off, and a server
// ignores the fields that are not for it.
//
//   - Ollama: "think": false, for models that can think.
//   - OpenAI-compatible: "thinking": {"type": "disabled"} (GLM, DeepSeek and
//     Kimi style), "enable_thinking": false (Qwen style) and
//     "chat_template_kwargs": {"enable_thinking": false} (servers that run
//     the model's chat template, such as vLLM and SGLang).
//
// Anthropic has none: its models think only when asked to, and this
// application never asks.
var noThinkingFields = map[Provider]map[string]json.RawMessage{
	ProviderOllama: {
		"think": json.RawMessage(`false`),
	},
	ProviderOpenAICompatible: {
		"thinking":             json.RawMessage(`{"type":"disabled"}`),
		"enable_thinking":      json.RawMessage(`false`),
		"chat_template_kwargs": json.RawMessage(`{"enable_thinking":false}`),
	},
}

// SupportsNoThinking reports whether the "No thinking" option does anything for
// the provider.
func SupportsNoThinking(p Provider) bool {
	_, ok := noThinkingFields[p]
	return ok
}

// noThinkingClient returns an HTTP client that adds the fields of the provider
// to the JSON body of every POST, or nil if the provider has none.
func noThinkingClient(p Provider) *http.Client {
	fields, ok := noThinkingFields[p]
	if !ok {
		return nil
	}
	return &http.Client{Transport: &fieldInjector{base: http.DefaultTransport, fields: fields}}
}

// fieldInjector is an http.RoundTripper that adds fields to the top level of a
// JSON object body. A field the body already has is left as it is, and a body
// that is not a JSON object goes through unchanged.
type fieldInjector struct {
	base   http.RoundTripper
	fields map[string]json.RawMessage
}

func (t *fieldInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) == nil && obj != nil {
		for k, v := range t.fields {
			if _, set := obj[k]; !set {
				obj[k] = v
			}
		}
		if merged, err := json.Marshal(obj); err == nil {
			body = merged
		}
	}

	// The caller's request must stay as it was, so send a copy.
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	out.ContentLength = int64(len(body))
	return t.base.RoundTrip(out)
}
