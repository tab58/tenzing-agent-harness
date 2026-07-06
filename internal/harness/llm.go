package harness

import (
	"fmt"

	provider "github.com/tab58/llm-providers"
	"github.com/tab58/llm-providers/common"
)

// defaultLLMFactory builds LLM clients via provider.LLMFromEnv, applying any
// configured per-provider base URL.
func defaultLLMFactory(baseURLs map[common.Provider]string) func(common.ModelDefinition) (common.LLM, error) {
	return func(model common.ModelDefinition) (common.LLM, error) {
		opts := []provider.Option{}
		if url := baseURLs[model.Provider]; url != "" {
			opts = append(opts, provider.WithBaseURL(url))
		}
		return provider.LLMFromEnv(model, opts...)
	}
}

// llmCache builds LLM clients on demand and reuses one client per distinct
// model, so roles that share a model also share a client (and its rate
// limiter).
type llmCache struct {
	factory  func(common.ModelDefinition) (common.LLM, error)
	baseURLs map[common.Provider]string
	clients  map[string]common.LLM
}

func newLLMCache(factory func(common.ModelDefinition) (common.LLM, error), baseURLs map[common.Provider]string) *llmCache {
	return &llmCache{
		factory:  factory,
		baseURLs: baseURLs,
		clients:  make(map[string]common.LLM),
	}
}

func (c *llmCache) get(model common.ModelDefinition) (common.LLM, error) {
	key := fmt.Sprintf("%s|%s|%s", model.Provider, model.Name, c.baseURLs[model.Provider])
	if llm, ok := c.clients[key]; ok {
		return llm, nil
	}
	llm, err := c.factory(model)
	if err != nil {
		return nil, err
	}
	c.clients[key] = llm
	return llm, nil
}
