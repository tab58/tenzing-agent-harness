package provider

import "testing"

func TestConstructorDefaults(t *testing.T) {
	tests := []struct {
		name      string
		llm       LLM
		wantModel string
	}{
		{
			"anthropic default",
			NewAnthropicClient(AnthropicConfig{APIKey: "k"}),
			string(AnthropicModelClaudeSonnet4_6),
		},
		{
			"openai default",
			NewOpenAIClient(OpenAIConfig{APIKey: "k"}),
			string(OpenAIModelGPT5_4),
		},
		{
			"cerebras default",
			NewCerebrasClient(CerebrasConfig{APIKey: "k"}),
			string(CerebrasModelGPTOSS120B),
		},
		{
			"cerebras override",
			NewCerebrasClient(CerebrasConfig{APIKey: "k", Model: CerebrasModel("custom")}),
			"custom",
		},
		{
			"lightning default",
			NewLightningClient(LightningConfig{APIKey: "k", BaseURL: "https://example.test"}),
			string(LightningModelGemma4_31B),
		},
		{
			"openrouter default",
			NewOpenRouterClient(OpenRouterConfig{APIKey: "k"}),
			string(OpenRouterModelGemma4_31B),
		},
		{
			"ollama default",
			NewOllamaClient(OllamaConfig{}),
			string(OllamaModelGemma4_31B),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.llm.GetCurrentModel(); got != tt.wantModel {
				t.Errorf("GetCurrentModel() = %q, want %q", got, tt.wantModel)
			}
		})
	}
}

func TestRateLimiterWiring(t *testing.T) {
	if l := NewAnthropicClient(AnthropicConfig{APIKey: "k"}).rateLimiter; l == nil {
		t.Error("anthropic: default rate limiter missing")
	}
	if l := NewAnthropicClient(AnthropicConfig{APIKey: "k"}, WithAnthropicNoRateLimit()).rateLimiter; l != nil {
		t.Error("anthropic: rate limiter should be nil when disabled")
	}

	if l := NewOpenAIClient(OpenAIConfig{APIKey: "k"}).rateLimiter; l == nil {
		t.Error("openai: default rate limiter missing")
	}
	if l := NewOpenAIClient(OpenAIConfig{APIKey: "k"}, WithOpenAINoRateLimit()).rateLimiter; l != nil {
		t.Error("openai: rate limiter should be nil when disabled")
	}

	if l := NewCerebrasClient(CerebrasConfig{APIKey: "k"}, WithCerebrasNoRateLimit()).rateLimiter; l != nil {
		t.Error("cerebras: rate limiter should be nil when disabled")
	}

	lightningCfg := LightningConfig{APIKey: "k", BaseURL: "https://example.test"}
	if l := NewLightningClient(lightningCfg, WithLightningNoRateLimit()).rateLimiter; l != nil {
		t.Error("lightning: rate limiter should be nil when disabled")
	}
	if !NewLightningClient(lightningCfg).retryRateLimit {
		t.Error("lightning: retryRateLimit should be enabled")
	}

	if !NewOpenAIClient(OpenAIConfig{APIKey: "k"}).useMaxCompletionTokens {
		t.Error("openai: useMaxCompletionTokens should be enabled")
	}
}
