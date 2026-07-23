// Written as an external test (package tenzing_test) so it proves a
// consumer can build and run an agent through the facade alone, without
// importing internal packages.
package tenzing_test

import (
	"context"
	"testing"

	"github.com/tab58/tenzing-agent-harness/pkg/tenzing"
)

type stubAgent struct{}

func (s *stubAgent) GetCurrentModel() string               { return "stub-model" }
func (s *stubAgent) UpdateStreamCallback(_ func(string))   {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string)) {}

func (s *stubAgent) DoReasoning(_ context.Context, _ []tenzing.Message, _ []string, _ []tenzing.LLMToolDefinition) (tenzing.ReasoningResult, error) {
	return tenzing.ReasoningResult{FinalAnswer: "done"}, nil
}

type stubLLM struct{}

func (s *stubLLM) SendSyncMessage(_ context.Context, _ tenzing.CompletionRequest) (tenzing.CompletionResponse, error) {
	return tenzing.CompletionResponse{}, nil
}
func (s *stubLLM) SendStreamingMessage(_ context.Context, _ tenzing.CompletionRequest, _ chan<- tenzing.StreamEvent) error {
	return nil
}
func (s *stubLLM) SendMessageWithTools(_ context.Context, _ tenzing.CompletionRequest, _ []tenzing.LLMToolDefinition) (tenzing.CompletionResponse, error) {
	return tenzing.CompletionResponse{}, nil
}
func (s *stubLLM) CountTokens(_ context.Context, _ tenzing.CompletionRequest) (tenzing.TokenCount, error) {
	return tenzing.TokenCount{}, nil
}
func (s *stubLLM) ListModels(_ context.Context) ([]tenzing.ModelInfo, error) {
	return nil, nil
}
func (s *stubLLM) GetCurrentModel() string        { return "stub" }
func (s *stubLLM) GetContextWindowSize() int      { return 4096 }
func (s *stubLLM) ProviderName() tenzing.Provider { return tenzing.ProviderOllama }

// customExt proves a consumer can implement the extension surface through
// the facade alone: an Extension with prompt, tool, and hook capabilities.
type customExt struct{ iterations int }

func (e *customExt) Name() string              { return "custom" }
func (e *customExt) PromptFragment() string    { return "custom fragment" }
func (e *customExt) Tools() []tenzing.ToolSpec { return nil }
func (e *customExt) BeforeIteration(_ context.Context, _ *tenzing.TurnContext) error {
	e.iterations++
	return nil
}

var (
	_ tenzing.Extension           = (*customExt)(nil)
	_ tenzing.PromptContributor   = (*customExt)(nil)
	_ tenzing.ToolProvider        = (*customExt)(nil)
	_ tenzing.BeforeIterationHook = (*customExt)(nil)
)

func TestFacadeRegistersCustomExtension(t *testing.T) {
	model := tenzing.ModelDefinition{Name: "stub-model", Provider: tenzing.ProviderOllama}
	ext := &customExt{}

	h, err := tenzing.New(model,
		tenzing.WithAgentBuilder(func(_ tenzing.LLM, _ string) (tenzing.Agent, error) {
			return &stubAgent{}, nil
		}),
		tenzing.WithLLMFactory(func(_ tenzing.ModelDefinition) (tenzing.LLM, error) {
			return &stubLLM{}, nil
		}),
		tenzing.WithSystemPrompt("test"),
		tenzing.WithExtension(ext),
	)
	if err != nil {
		t.Fatalf("tenzing.New() error: %v", err)
	}
	defer h.Shutdown()

	if _, err := h.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn() error: %v", err)
	}
	if ext.iterations == 0 {
		t.Error("custom extension's BeforeIteration hook never ran")
	}
}

func TestFacadeRunsSingleLoop(t *testing.T) {
	model := tenzing.ModelDefinition{Name: "stub-model", Provider: tenzing.ProviderOllama}

	h, err := tenzing.New(model,
		tenzing.WithAgentBuilder(func(_ tenzing.LLM, _ string) (tenzing.Agent, error) {
			return &stubAgent{}, nil
		}),
		tenzing.WithLLMFactory(func(_ tenzing.ModelDefinition) (tenzing.LLM, error) {
			return &stubLLM{}, nil
		}),
		tenzing.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("tenzing.New() error: %v", err)
	}
	defer h.Shutdown()

	answer, err := h.RunTurn(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunTurn() error: %v", err)
	}
	if answer != "done" {
		t.Errorf("RunTurn() = %q, want %q", answer, "done")
	}
}
