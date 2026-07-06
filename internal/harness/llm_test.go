package harness

import (
	"fmt"
	"testing"

	"github.com/tab58/llm-providers/common"
)

func TestLLMCacheReusesClientPerModel(t *testing.T) {
	calls := 0
	factory := func(m common.ModelDefinition) (common.LLM, error) {
		calls++
		return &stubLLM{}, nil
	}
	cache := newLLMCache(factory, nil)

	modelA := common.ModelDefinition{Name: "a", Provider: common.ProviderOllama}
	modelB := common.ModelDefinition{Name: "b", Provider: common.ProviderOllama}

	llm1, err := cache.get(modelA)
	if err != nil {
		t.Fatal(err)
	}
	llm2, err := cache.get(modelA)
	if err != nil {
		t.Fatal(err)
	}
	if llm1 != llm2 {
		t.Error("same model should return the same client instance")
	}
	if calls != 1 {
		t.Errorf("factory called %d times for one model, want 1", calls)
	}

	if _, err := cache.get(modelB); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("factory called %d times for two models, want 2", calls)
	}
}

func TestLLMCachePropagatesFactoryError(t *testing.T) {
	factory := func(m common.ModelDefinition) (common.LLM, error) {
		return nil, fmt.Errorf("boom")
	}
	cache := newLLMCache(factory, nil)
	if _, err := cache.get(common.ModelDefinition{Name: "x", Provider: common.ProviderOpenAI}); err == nil {
		t.Fatal("expected factory error to propagate")
	}
}
