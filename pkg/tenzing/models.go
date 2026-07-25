package tenzing

import (
	"github.com/tab58/llm-providers/anthropic"
	"github.com/tab58/llm-providers/cerebras"
	"github.com/tab58/llm-providers/common"
	"github.com/tab58/llm-providers/lightning"
	"github.com/tab58/llm-providers/ollama"
	"github.com/tab58/llm-providers/openai"
	"github.com/tab58/llm-providers/openrouter"
)

// Model and provider types from llm-providers.
type (
	Model           = common.Model
	ModelDefinition = common.ModelDefinition
	Provider        = common.Provider
)

// Providers.
const (
	ProviderAnthropic  = common.ProviderAnthropic
	ProviderCerebras   = common.ProviderCerebras
	ProviderLightning  = common.ProviderLightning
	ProviderOllama     = common.ProviderOllama
	ProviderOpenAI     = common.ProviderOpenAI
	ProviderOpenRouter = common.ProviderOpenRouter
)

// Standard provider models, prefixed by provider because names collide
// across providers. Asserted to ModelDefinition so they pass straight into
// New, which takes the concrete type rather than the Model interface.
var (
	Anthropic_ClaudeOpus4_6   = anthropic.Model_ClaudeOpus4_6.(ModelDefinition)
	Anthropic_ClaudeSonnet4_6 = anthropic.Model_ClaudeSonnet4_6.(ModelDefinition)
	Anthropic_ClaudeHaiku4_5  = anthropic.Model_ClaudeHaiku4_5.(ModelDefinition)

	OpenAI_GPT5_4     = openai.Model_GPT5_4.(ModelDefinition)
	OpenAI_GPT5_4Mini = openai.Model_GPT5_4Mini.(ModelDefinition)

	Cerebras_GPTOSS_120B = cerebras.Model_GPTOSS_120B.(ModelDefinition)

	Lightning_Gemma4_31B  = lightning.Model_Gemma4_31B.(ModelDefinition)
	Lightning_GPTOSS_120B = lightning.Model_GPTOSS_120B.(ModelDefinition)

	OpenRouter_Gemma4_31B = openrouter.Model_Gemma4_31B.(ModelDefinition)

	Ollama_GLM5_2_Cloud = ollama.Model_GLM5_2_Cloud.(ModelDefinition)
	Ollama_Qwen3_5_9B   = ollama.Model_Qwen3_5_9B.(ModelDefinition)
	Ollama_Qwen3_5_35B  = ollama.Model_Qwen3_5_35B.(ModelDefinition)
	Ollama_Qwen3_5_122B = ollama.Model_Qwen3_5_122B.(ModelDefinition)
	Ollama_Gemma4_31B   = ollama.Model_Gemma4_31B.(ModelDefinition)
)

// StandardModels returns every standard model re-exported above — the
// canonical list consumers (and cmd/app's model registry) derive from.
// Add new re-exported models here in the same change.
func StandardModels() []ModelDefinition {
	return []ModelDefinition{
		Anthropic_ClaudeOpus4_6,
		Anthropic_ClaudeSonnet4_6,
		Anthropic_ClaudeHaiku4_5,
		OpenAI_GPT5_4,
		OpenAI_GPT5_4Mini,
		Cerebras_GPTOSS_120B,
		Lightning_Gemma4_31B,
		Lightning_GPTOSS_120B,
		OpenRouter_Gemma4_31B,
		Ollama_GLM5_2_Cloud,
		Ollama_Qwen3_5_9B,
		Ollama_Qwen3_5_35B,
		Ollama_Qwen3_5_122B,
		Ollama_Gemma4_31B,
	}
}
