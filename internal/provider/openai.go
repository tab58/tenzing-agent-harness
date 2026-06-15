package provider

import (
	"tenzing-agent/internal/provider/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type OpenAIModel string

const (
	OpenAIModelGPT5_4     = OpenAIModel(openai.ChatModelGPT5_4)
	OpenAIModelGPT5_4Mini = OpenAIModel(openai.ChatModelGPT5_4Mini)

	MaxTokensGPT5_4     int64 = 128000
	MaxTokensGPT5_4Mini int64 = 64000
)

// OpenAI implements the LLM interface using the OpenAI API.
type OpenAI struct {
	*openAICompat
}

// OpenAIConfig holds configuration for connecting to the OpenAI API.
type OpenAIConfig struct {
	APIKey string
	Model  OpenAIModel
}

type openaiOptions struct {
	rateLimiter *utils.TokenBucket
	haveLimiter bool
}

// OpenAIOption is a functional option for configuring the OpenAI client.
type OpenAIOption func(*openaiOptions)

// WithOpenAINoRateLimit disables rate limiting for the OpenAI client.
func WithOpenAINoRateLimit() OpenAIOption {
	return func(o *openaiOptions) {
		o.rateLimiter = nil
		o.haveLimiter = true
	}
}

// NewOpenAIClient creates an OpenAI LLM client.
func NewOpenAIClient(cfg OpenAIConfig, opts ...OpenAIOption) *OpenAI {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
	)
	model := cfg.Model
	if model == "" {
		model = OpenAIModelGPT5_4
	}

	var o openaiOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !o.haveLimiter {
		o.rateLimiter = utils.NewTokenBucket(utils.TokenBucketConfig{
			Rate:           10_000.0 / 60.0, // 10K input tokens per minute
			BurstSize:      10_000,
			MaxConcurrency: 10,
		})
	}

	return &OpenAI{&openAICompat{
		name:                   "openai",
		client:                 &client,
		model:                  string(model),
		rateLimiter:            o.rateLimiter,
		tokenCostLimit:         true,
		useMaxCompletionTokens: true,
	}}
}
