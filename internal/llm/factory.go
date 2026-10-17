package llm

import (
	"net/http"

	"github.com/rm4n0s/errors"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// noKey is sent to OpenAI-compatible servers that need no key (local servers):
// langchaingo refuses an empty token and would otherwise read OPENAI_API_KEY
// from the environment.
const noKey = "not-needed"

// builder constructs the langchaingo model of one provider from a validated
// configuration.
type builder func(cfg ModelConfig) (llms.Model, error)

func defaultBuilders() map[Provider]builder {
	return map[Provider]builder{
		ProviderOllama: func(cfg ModelConfig) (llms.Model, error) {
			opts := []ollama.Option{
				ollama.WithModel(cfg.Model),
				ollama.WithServerURL(cfg.BaseURL),
			}
			if c := noThinkingClientIf(cfg); c != nil {
				opts = append(opts, ollama.WithHTTPClient(c))
			}
			return ollama.New(opts...)
		},
		ProviderOpenAICompatible: func(cfg ModelConfig) (llms.Model, error) {
			key := cfg.APIKey
			if key == "" {
				key = noKey
			}
			opts := []openai.Option{
				openai.WithModel(cfg.Model),
				openai.WithBaseURL(cfg.BaseURL),
				openai.WithToken(key),
			}
			if c := noThinkingClientIf(cfg); c != nil {
				opts = append(opts, openai.WithHTTPClient(c))
			}
			return openai.New(opts...)
		},
		ProviderAnthropic: func(cfg ModelConfig) (llms.Model, error) {
			opts := []anthropic.Option{
				anthropic.WithModel(cfg.Model),
				anthropic.WithToken(cfg.APIKey),
			}
			if cfg.BaseURL != "" {
				opts = append(opts, anthropic.WithBaseURL(cfg.BaseURL))
			}
			return anthropic.New(opts...)
		},
	}
}

// New validates cfg and builds the langchaingo model for its provider. Every
// credential and URL is passed explicitly, so environment variables such as
// OPENAI_API_KEY or ANTHROPIC_API_KEY are never picked up.
//
// The temperature is not part of the model: it is a per-call option, see
// Request.Temperature.
func New(cfg ModelConfig) (llms.Model, error) {
	return newWith(cfg, defaultBuilders())
}

func newWith(cfg ModelConfig, builders map[Provider]builder) (llms.Model, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	build, ok := builders[cfg.Provider]
	if !ok {
		return nil, errors.New("UnknownProvider", "Provider must be ollama, openai-compatible or anthropic.", "provider", string(cfg.Provider))
	}
	m, err := build(cfg)
	if err != nil {
		return nil, clientCreateFailed(err, cfg)
	}
	return m, nil
}

func clientCreateFailed(err error, cfg ModelConfig) error {
	return errors.NewErr("LLMClientCreateFailed", err).
		SetMetadata("provider", string(cfg.Provider), "model", cfg.Model)
}

// noThinkingClientIf returns the HTTP client for cfg when it asks for no
// thinking, and nil when it does not or the provider has nothing to send.
func noThinkingClientIf(cfg ModelConfig) *http.Client {
	if !cfg.NoThinking {
		return nil
	}
	return noThinkingClient(cfg.Provider)
}
