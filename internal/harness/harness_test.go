package harness

import (
	"context"
	"testing"

	"tenzing-agent/internal/provider"
)

type stubAgent struct{}

func (s *stubAgent) UpdateToolDefinitions(_ []provider.ToolDefinition) {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)              {}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (ReasoningResult, error) {
	return ReasoningResult{FinalAnswer: "done"}, nil
}

type stubLLM struct{}

func (s *stubLLM) SendSyncMessage(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, nil
}
func (s *stubLLM) SendStreamingMessage(_ context.Context, _ provider.CompletionRequest, _ chan<- provider.StreamEvent) error {
	return nil
}
func (s *stubLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, nil
}
func (s *stubLLM) CountTokens(_ context.Context, _ provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{}, nil
}
func (s *stubLLM) ListModels(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (s *stubLLM) GetCurrentModel() string    { return "stub" }
func (s *stubLLM) GetContextWindowSize() int { return 4096 }

func TestHarnessCreatesRunner(t *testing.T) {
	_, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMRootLLM:       &stubLLM{},
		MainSystemPrompt: "test prompt",
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
}
