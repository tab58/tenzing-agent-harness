package provider

import (
	"tenzing-agent/internal/provider/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type LightningModel string

const (
	LightningModelGemma4_31B  = LightningModel("lightning-ai/gemma-4-31B-it")
	LightningModelGPTOSS_120B = LightningModel("lightning-ai/gpt-oss-120b")

	MaxTokensLightningGemma4_31B int64 = 245000
)

// Lightning implements the LLM interface using Lightning.ai's OpenAI-compatible API.
type Lightning struct {
	*openAICompat
}

// LightningConfig holds configuration for connecting to the Lightning.ai API.
// BaseURL is required: Lightning.ai endpoints are deployment-specific.
type LightningConfig struct {
	APIKey  string
	BaseURL string
	Model   LightningModel
}

type lightningOptions struct {
	rateLimiter *utils.TokenBucket
	haveLimiter bool
}

// LightningOption is a functional option for configuring the Lightning client.
type LightningOption func(*lightningOptions)

// WithLightningNoRateLimit disables rate limiting for the Lightning client.
func WithLightningNoRateLimit() LightningOption {
	return func(o *lightningOptions) {
		o.rateLimiter = nil
		o.haveLimiter = true
	}
}

// NewLightningClient creates a Lightning.ai LLM client using the
// OpenAI-compatible API. Requests rejected with HTTP 429 are retried with
// exponential backoff.
func NewLightningClient(cfg LightningConfig, opts ...LightningOption) *Lightning {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
	)
	model := cfg.Model
	if model == "" {
		model = LightningModelGemma4_31B
	}

	var o lightningOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !o.haveLimiter {
		o.rateLimiter = utils.NewTokenBucket(utils.TokenBucketConfig{
			Rate:           0.25, // 15 requests per minute
			BurstSize:      3,    // small burst to avoid hitting server-side limits
			MaxConcurrency: 3,
		})
	}

	return &Lightning{&openAICompat{
		name:           "lightning",
		client:         &client,
		model:          string(model),
		rateLimiter:    o.rateLimiter,
		retryRateLimit: true,
	}}
}
