package harness

import (
	"context"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/provider"
)

type stubAgent struct{}

func (s *stubAgent) UpdateToolDefinitions(_ []provider.ToolDefinition)                              {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)                                             {}
func (s *stubAgent) UpdateOffloadFn(_ func(context.Context, string) (string, error))                {}
func (s *stubAgent) UpdateStreamCallback(_ func(string))                                            {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string))                                          {}
func (s *stubAgent) SetTodoProvider(_ func() string)                                                {}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (runner.ReasoningResult, error) {
	return runner.ReasoningResult{FinalAnswer: "done"}, nil
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
func (s *stubLLM) GetCurrentModel() string   { return "stub" }
func (s *stubLLM) GetContextWindowSize() int { return 4096 }

func TestHarnessCreatesRunner(t *testing.T) {
	_, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		MainSystemPrompt: "test prompt",
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
}

func TestHarnessRegistersSpawnAgentWhenEnabled(t *testing.T) {
	h, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		SubAgentMaxDepth: 2,
		SubAgentBuilder: func(llm provider.LLM, sp string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
		MainSystemPrompt: "test",
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	found := false
	for _, def := range h.ToolDefinitions() {
		if def.Name() == "spawn_agent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("spawn_agent tool not registered when SubAgentMaxDepth > 0")
	}
}

func TestHarnessNoSpawnAgentWhenDisabled(t *testing.T) {
	h, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		MainSystemPrompt: "test",
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	for _, def := range h.ToolDefinitions() {
		if def.Name() == "spawn_agent" {
			t.Fatal("spawn_agent tool should not be registered when SubAgentMaxDepth is 0")
		}
	}
}

func TestHarnessCreatesEventBus(t *testing.T) {
	h, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		MainSystemPrompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.EventBus() == nil {
		t.Fatal("EventBus() should not be nil")
	}
}

func TestHarnessEmitsTurnEventsOnRunTurn(t *testing.T) {
	h, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		MainSystemPrompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ch := h.EventBus().Subscribe(50)

	_, err = h.RunTurn(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}

	var types []events.EventType
	for {
		select {
		case ev := <-ch:
			types = append(types, ev.Type())
		default:
			goto check
		}
	}
check:
	hasType := func(et events.EventType) bool {
		for _, t := range types {
			if t == et {
				return true
			}
		}
		return false
	}
	if !hasType(events.EventTurnStarted) {
		t.Error("missing TurnStarted event")
	}
	if !hasType(events.EventTurnCompleted) {
		t.Error("missing TurnCompleted event")
	}
}
