package llm

import "testing"

func TestUsageFromInfo(t *testing.T) {
	tests := []struct {
		name   string
		info   map[string]any
		want   Usage
		wantOK bool
	}{
		{"nil map", nil, Usage{}, false},
		{"empty map", map[string]any{}, Usage{}, false},
		{"openai and ollama keys", map[string]any{"PromptTokens": 10, "CompletionTokens": 4, "TotalTokens": 14}, Usage{InputTokens: 10, OutputTokens: 4}, true},
		{"anthropic keys", map[string]any{"InputTokens": 7, "OutputTokens": 3, "CacheReadInputTokens": 0}, Usage{InputTokens: 7, OutputTokens: 3}, true},
		{"int64 values", map[string]any{"PromptTokens": int64(5), "CompletionTokens": int64(2)}, Usage{InputTokens: 5, OutputTokens: 2}, true},
		{"float64 values (decoded JSON)", map[string]any{"InputTokens": 5.0, "OutputTokens": 2.0}, Usage{InputTokens: 5, OutputTokens: 2}, true},
		{"only output reported", map[string]any{"CompletionTokens": 9}, Usage{OutputTokens: 9}, true},
		{"zeros mean not reported", map[string]any{"PromptTokens": 0, "CompletionTokens": 0}, Usage{}, false},
		{"unrelated keys", map[string]any{"ThinkingTokens": 3}, Usage{}, false},
		{"wrong value type", map[string]any{"PromptTokens": "10"}, Usage{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := UsageFromInfo(tt.info)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("UsageFromInfo() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestEstimateUsage(t *testing.T) {
	cases := []struct {
		name         string
		in, out      string
		wantIn, wOut int
	}{
		{"empty", "", "", 0, 0},
		{"one char rounds up", "a", "ab", 1, 1},
		{"four chars", "abcd", "abcde", 1, 2},
		{"counts runes not bytes", "ééééé", "日本語", 2, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateUsage(tt.in, tt.out)
			if got.InputTokens != tt.wantIn || got.OutputTokens != tt.wOut || !got.Estimated {
				t.Errorf("EstimateUsage() = %+v, want in=%d out=%d estimated", got, tt.wantIn, tt.wOut)
			}
		})
	}
}

func TestUsageAdd(t *testing.T) {
	tests := []struct {
		name string
		a, b Usage
		want Usage
	}{
		{"exact plus exact", Usage{1, 2, false}, Usage{3, 4, false}, Usage{4, 6, false}},
		{"estimate taints the sum", Usage{1, 2, false}, Usage{3, 4, true}, Usage{4, 6, true}},
		{"zero value", Usage{}, Usage{5, 6, false}, Usage{5, 6, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Add(tt.b); got != tt.want {
				t.Errorf("Add() = %+v, want %+v", got, tt.want)
			}
			if tt.want.Total() != tt.want.InputTokens+tt.want.OutputTokens {
				t.Error("Total() mismatch")
			}
		})
	}
}
