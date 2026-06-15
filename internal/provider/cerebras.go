package provider

import (
	"tenzing-agent/internal/provider/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type CerebrasModel string

const (
	cerebrasBaseURL = "https://api.cerebras.ai/v1"

	CerebrasModelGPTOSS120B = CerebrasModel("gpt-oss-120b")

	MaxTokensCerebrasGPTOSS120B int64 = 128000
)

// Cerebras implements the LLM interface using Cerebras's OpenAI-compatible API.
type Cerebras struct {
	*openAICompat
}

// CerebrasConfig holds configuration for connecting to the Cerebras API.
type CerebrasConfig struct {
	APIKey string
	Model  CerebrasModel
}

type cerebrasOptions struct {
	rateLimiter *utils.TokenBucket
	haveLimiter bool
}

// CerebrasOption is a functional option for configuring the Cerebras client.
type CerebrasOption func(*cerebrasOptions)

// WithCerebrasNoRateLimit disables rate limiting for the Cerebras client.
func WithCerebrasNoRateLimit() CerebrasOption {
	return func(o *cerebrasOptions) {
		o.rateLimiter = nil
		o.haveLimiter = true
	}
}

// NewCerebrasClient creates a Cerebras LLM client using the OpenAI-compatible API.
func NewCerebrasClient(cfg CerebrasConfig, opts ...CerebrasOption) *Cerebras {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cerebrasBaseURL),
	)
	model := cfg.Model
	if model == "" {
		model = CerebrasModelGPTOSS120B
	}

	var o cerebrasOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !o.haveLimiter {
		o.rateLimiter = utils.NewTokenBucket(utils.TokenBucketConfig{
			Rate:           30.0 / 60.0, // 30 requests per minute
			BurstSize:      1,
			MaxConcurrency: 10,
		})
	}

	return &Cerebras{&openAICompat{
		name:        "cerebras",
		client:      &client,
		model:       string(model),
		rateLimiter: o.rateLimiter,
	}}
}
