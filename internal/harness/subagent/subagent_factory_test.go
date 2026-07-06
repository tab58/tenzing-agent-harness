package subagent

import (
	"context"
	"testing"

	"tenzing-agent/internal/harness/runner"

	"github.com/tab58/llm-providers/common"
)

type stubLLM struct{}

func (s *stubLLM) SendSyncMessage(_ context.Context, _ common.CompletionRequest) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}
func (s *stubLLM) SendStreamingMessage(_ context.Context, _ common.CompletionRequest, _ chan<- common.StreamEvent) error {
	return nil
}
func (s *stubLLM) SendMessageWithTools(_ context.Context, _ common.CompletionRequest, _ []common.ToolDefinition) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}
func (s *stubLLM) CountTokens(_ context.Context, _ common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{}, nil
}
func (s *stubLLM) ListModels(_ context.Context) ([]common.ModelInfo, error) {
	return nil, nil
}
func (s *stubLLM) GetCurrentModel() string       { return "stub" }
func (s *stubLLM) GetContextWindowSize() int     { return 4096 }
func (s *stubLLM) ProviderName() common.Provider { return common.ProviderOllama }

type stubAgent struct{}

func (s *stubAgent) GetCurrentModel() string                                         { return "stub" }
func (s *stubAgent) UpdateToolDefinitions(_ []common.ToolDefinition)                 {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)                              {}
func (s *stubAgent) UpdateOffloadFn(_ func(context.Context, string) (string, error)) {}
func (s *stubAgent) UpdateStreamCallback(_ func(string))                             {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string))                           {}
func (s *stubAgent) SetTodoProvider(_ func() string)                                 {}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (runner.ReasoningResult, error) {
	return runner.ReasoningResult{FinalAnswer: "done"}, nil
}

func TestNewSubAgentFactory(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      2,
		MaxIterations: 10,
		Cwd:           t.TempDir(),
		AgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
	})
	if factory == nil {
		t.Fatal("expected non-nil factory")
	}
}

func TestSubAgentFactoryDefaults(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM: &stubLLM{},
		Cwd:      t.TempDir(),
	})
	if factory.maxDepth != 2 {
		t.Fatalf("maxDepth = %d, want 2", factory.maxDepth)
	}
	if factory.maxIterations != 30 {
		t.Fatalf("maxIterations = %d, want 30", factory.maxIterations)
	}
}

func TestSubAgentFactorySpawnsAgent(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      1,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
		AgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
	})

	result, err := factory.SpawnAgent(context.Background(), "do the thing", "some context")
	if err != nil {
		t.Fatalf("SpawnAgent error: %v", err)
	}
	if result != "done" {
		t.Fatalf("got %q, want %q", result, "done")
	}
}

func TestSubAgentFactoryChildAtMaxDepthHasNoSpawnAgent(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      1,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
	})

	registry := factory.buildChildToolRegistry()
	for _, def := range registry.Definitions() {
		if def.Name() == "spawn_agent" {
			t.Fatal("child at max depth should not have spawn_agent tool")
		}
	}
}

func TestSubAgentFactoryChildBelowMaxDepthHasSpawnAgent(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      2,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
		AgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
	})

	registry := factory.buildChildToolRegistry()
	found := false
	for _, def := range registry.Definitions() {
		if def.Name() == "spawn_agent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("child below max depth should have spawn_agent tool")
	}
}

func TestSubAgentFactoryChildHasRLMTool(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      1,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
	})

	registry := factory.buildChildToolRegistry()
	found := false
	for _, def := range registry.Definitions() {
		if def.Name() == "rlm" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("child should have rlm tool")
	}
}

func TestSubAgentFactoryImmutability(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      2,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
	})

	depthBefore := factory.currentDepth
	_ = factory.buildChildToolRegistry()

	if factory.currentDepth != depthBefore {
		t.Fatalf("factory currentDepth mutated: was %d, now %d", depthBefore, factory.currentDepth)
	}
}
