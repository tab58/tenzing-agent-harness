package harness

import (
	"context"
	"testing"

	"tenzing-agent/internal/harness/tools"
)

type stubAgent struct{}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (ReasoningResult, error) {
	return ReasoningResult{FinalAnswer: "done"}, nil
}

func TestHarnessCreatesRunner(t *testing.T) {
	registry := tools.NewRegistry(t.TempDir())

	_, err := New(HarnessConfig{
		MainRunner: AgentRunnerConfig{
			Agent:        &stubAgent{},
			ToolRegistry: registry,
			SystemPrompt: "test prompt",
		},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
}
