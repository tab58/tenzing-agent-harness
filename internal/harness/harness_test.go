package harness

import (
	"context"
	"testing"

	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/provider"
)

type stubLLM struct{}

func (s *stubLLM) SendSyncMessage(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{
		Content: []provider.ContentBlock{provider.NewTextContent("stub response")},
	}, nil
}

func (s *stubLLM) SendStreamingMessage(context.Context, provider.CompletionRequest, chan<- provider.StreamEvent) error {
	return provider.ErrNotSupported
}

func (s *stubLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, provider.ErrNotSupported
}

func (s *stubLLM) CountTokens(context.Context, provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{}, provider.ErrNotSupported
}

func (s *stubLLM) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return nil, provider.ErrNotSupported
}

func (s *stubLLM) GetCurrentModel() string      { return "stub-model" }
func (s *stubLLM) GetContextWindowSize() int { return 128_000 }

type stubAgent struct{}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (ReasoningResult, error) {
	return ReasoningResult{FinalAnswer: "done"}, nil
}

func TestHarnessRegistersSubLM(t *testing.T) {
	registry := tools.NewRegistry(t.TempDir())

	_, err := New(HarnessConfig{
		MainRunner: AgentRunnerConfig{
			Agent:        &stubAgent{},
			ToolRegistry: registry,
			SystemPrompt: "test prompt",
		},
		SubLMModel: &stubLLM{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := registry.Execute(context.Background(), "sub_lm", `{"prompt":"test"}`)
	if err != nil {
		t.Fatalf("sub_lm not registered: %v", err)
	}
	if result.IsError {
		t.Fatalf("sub_lm returned error: %s", result.Output)
	}
	if result.Output != "stub response" {
		t.Fatalf("got %q, want %q", result.Output, "stub response")
	}
}

func TestHarnessNoSubLMWithoutModel(t *testing.T) {
	registry := tools.NewRegistry(t.TempDir())

	_, err := New(HarnessConfig{
		MainRunner: AgentRunnerConfig{
			Agent:        &stubAgent{},
			ToolRegistry: registry,
			SystemPrompt: "test prompt",
		},
		SubLMModel: nil,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := registry.Execute(context.Background(), "sub_lm", `{"prompt":"test"}`)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.IsError {
		t.Fatal("sub_lm should not be registered when SubLMModel is nil")
	}
}

func TestHarnessSubLMAddsGuidanceToPrompt(t *testing.T) {
	registry := tools.NewRegistry(t.TempDir())
	basePrompt := "base system prompt"

	h, err := New(HarnessConfig{
		MainRunner: AgentRunnerConfig{
			Agent:        &stubAgent{},
			ToolRegistry: registry,
			SystemPrompt: basePrompt,
		},
		SubLMModel: &stubLLM{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sp := h.SystemPrompt()
	if sp == basePrompt {
		t.Fatal("system prompt not modified — RLM guidance not appended")
	}
	if len(sp) <= len(basePrompt) {
		t.Fatal("system prompt should be longer with RLM guidance")
	}
}
