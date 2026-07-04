package harness

import (
	"context"
	"strings"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"

	"github.com/tab58/llm-providers/common"
)

type stubAgent struct{}

func (s *stubAgent) GetCurrentModel() string                                         { return "stub-model" }
func (s *stubAgent) UpdateToolDefinitions(_ []common.ToolDefinition)                 {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)                              {}
func (s *stubAgent) UpdateOffloadFn(_ func(context.Context, string) (string, error)) {}
func (s *stubAgent) UpdateStreamCallback(_ func(string))                             {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string))                           {}
func (s *stubAgent) SetTodoProvider(_ func() string)                                 {}

func (s *stubAgent) DoReasoning(_ context.Context, _ []string, _ []string) (runner.ReasoningResult, error) {
	return runner.ReasoningResult{FinalAnswer: "done"}, nil
}

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
		SubAgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
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

func TestHarnessAdvisorRegistration(t *testing.T) {
	tests := []struct {
		name         string
		advisorModel common.LLM
		enabled      bool
		want         bool
	}{
		{"enabled with model", &stubLLM{}, true, true},
		{"model set but not enabled (default off)", &stubLLM{}, false, false},
		{"enabled but no model", nil, true, false},
		{"neither", nil, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(HarnessConfig{
				Agent:            &stubAgent{},
				RLMModel:         &stubLLM{},
				AdvisorModel:     tt.advisorModel,
				EnableAdvisor:    tt.enabled,
				MainSystemPrompt: "test",
			})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			found := false
			for _, def := range h.ToolDefinitions() {
				if def.Name() == "advisor" {
					found = true
					break
				}
			}
			if found != tt.want {
				t.Errorf("advisor registered = %v, want %v", found, tt.want)
			}
		})
	}
}

func TestHarnessDisabledToolsRemovesBuiltins(t *testing.T) {
	h, err := New(HarnessConfig{
		Agent:            &stubAgent{},
		RLMModel:         &stubLLM{},
		MainSystemPrompt: "test",
		DisabledTools:    []string{"bash", "edit"},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	names := make(map[string]bool)
	for _, def := range h.ToolDefinitions() {
		names[strings.ToLower(def.Name())] = true
	}
	for _, banned := range []string{"bash", "edit"} {
		if names[banned] {
			t.Errorf("tool %q present despite DisabledTools", banned)
		}
	}
	for _, required := range []string{"read", "grep", "glob"} {
		if !names[required] {
			t.Errorf("tool %q missing; DisabledTools removed too much", required)
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
