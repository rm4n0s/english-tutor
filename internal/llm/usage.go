package llm

import "unicode/utf8"

// Usage is the number of tokens one request used.
type Usage struct {
	InputTokens  int
	OutputTokens int
	// Estimated is true when the provider reported no counts and they were
	// derived from the text length (about 4 characters per token).
	Estimated bool
}

// Total returns input plus output tokens.
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// Add returns the sum of two usages. The result is estimated if either is.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + o.InputTokens,
		OutputTokens: u.OutputTokens + o.OutputTokens,
		Estimated:    u.Estimated || o.Estimated,
	}
}

// Key names that langchaingo v0.1.15 puts in ContentChoice.GenerationInfo,
// checked against its source:
//
//   - llms/openai (also OpenAI-compatible servers): "PromptTokens" and
//     "CompletionTokens"
//   - llms/ollama: "PromptTokens" and "CompletionTokens" (Ollama's
//     prompt_eval_count and eval_count)
//   - llms/anthropic: "InputTokens" and "OutputTokens"
//
// All values are ints. When the pinned version changes, re-check these keys.
var (
	inputKeys  = []string{"InputTokens", "PromptTokens"}
	outputKeys = []string{"OutputTokens", "CompletionTokens"}
)

// UsageFromInfo reads token counts from a GenerationInfo map. It reports false
// if the map holds no counts, or only zeros (servers that do not report usage
// send zeros), so the caller can fall back to EstimateUsage.
func UsageFromInfo(info map[string]any) (Usage, bool) {
	in, inOK := firstCount(info, inputKeys)
	out, outOK := firstCount(info, outputKeys)
	if (!inOK && !outOK) || (in == 0 && out == 0) {
		return Usage{}, false
	}
	return Usage{InputTokens: in, OutputTokens: out}, true
}

func firstCount(info map[string]any, keys []string) (int, bool) {
	for _, k := range keys {
		v, ok := info[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case int:
			return n, true
		case int32:
			return int(n), true
		case int64:
			return int(n), true
		case float64:
			return int(n), true
		}
	}
	return 0, false
}

// EstimateUsage guesses token counts from text length: one token per four
// characters, rounded up.
func EstimateUsage(input, output string) Usage {
	return Usage{
		InputTokens:  estimateTokens(input),
		OutputTokens: estimateTokens(output),
		Estimated:    true,
	}
}

func estimateTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}
