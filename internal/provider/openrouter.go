package provider

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type OpenRouterModel string

const (
	OpenRouterModelGemma4_31B = OpenRouterModel("google/gemma-4-31b-it")
	openRouterBaseURL         = "https://openrouter.ai/api/v1"
)

// OpenRouter implements the LLM interface using OpenRouter's
// OpenAI-compatible API.
type OpenRouter struct {
	*openAICompat
}

// OpenRouterConfig holds configuration for connecting to the OpenRouter API.
type OpenRouterConfig struct {
	APIKey string
	Model  OpenRouterModel
}

// NewOpenRouterClient creates an OpenRouter LLM client using the
// OpenAI-compatible API.
func NewOpenRouterClient(cfg OpenRouterConfig) *OpenRouter {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(openRouterBaseURL),
	)
	model := cfg.Model
	if model == "" {
		model = OpenRouterModelGemma4_31B
	}

	return &OpenRouter{&openAICompat{
		name:   "openrouter",
		client: &client,
		model:  string(model),
	}}
}
