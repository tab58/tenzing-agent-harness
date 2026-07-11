package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"

	"github.com/tab58/llm-providers/common"
)

type stubAgent struct{}

func (s *stubAgent) GetCurrentModel() string                                         { return "stub-model" }
func (s *stubAgent) UpdateToolDefinitions(_ []common.ToolDefinition)                 {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)                              {}
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
func (s *stubLLM) GetCurrentModel() string       { return "stub" }
func (s *stubLLM) GetContextWindowSize() int     { return 4096 }
func (s *stubLLM) ProviderName() common.Provider { return common.ProviderOllama }

var testModel = common.ModelDefinition{Name: "stub-model", Provider: common.ProviderOllama}

func stubBrain(_ common.LLM, _ string) (runner.Agent, error) { return &stubAgent{}, nil }

func stubFactory(_ common.ModelDefinition) (common.LLM, error) { return &stubLLM{}, nil }

// newTestHarness builds a harness with stubbed LLMs and brain.
func newTestHarness(t *testing.T, opts ...HarnessOption) *Harness {
	t.Helper()
	h, err := New(testModel, append([]HarnessOption{
		WithAgentBuilder(stubBrain),
		WithLLMFactory(stubFactory),
		WithSystemPrompt("test"),
	}, opts...)...)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return h
}

func TestHarnessCreatesRunner(t *testing.T) {
	newTestHarness(t)
}

func TestHarnessRegistersSpawnAgentByDefault(t *testing.T) {
	h := newTestHarness(t)
	found := false
	for _, def := range h.ToolDefinitions() {
		if def.Name() == "spawn_agent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("spawn_agent tool not registered by default (depth 1)")
	}
}

func TestHarnessNoSpawnAgentWhenDepthZero(t *testing.T) {
	h := newTestHarness(t, WithSubagentDepth(0))
	for _, def := range h.ToolDefinitions() {
		if def.Name() == "spawn_agent" {
			t.Fatal("spawn_agent tool should not be registered when depth is 0")
		}
	}
}

func TestHarnessAdvisorRegistration(t *testing.T) {
	tests := []struct {
		name string
		opts []HarnessOption
		want bool
	}{
		{"advisor model set", []HarnessOption{WithAdvisorModel(testModel)}, true},
		{"no advisor model", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHarness(t, tt.opts...)
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
	h := newTestHarness(t, WithDisabledTool("bash"), WithDisabledTool("edit"))
	names := make(map[string]bool)
	for _, def := range h.ToolDefinitions() {
		names[strings.ToLower(def.Name())] = true
	}
	for _, banned := range []string{"bash", "edit"} {
		if names[banned] {
			t.Errorf("tool %q present despite WithDisabledTool", banned)
		}
	}
	for _, required := range []string{"read", "grep", "glob"} {
		if !names[required] {
			t.Errorf("tool %q missing; WithDisabledTool removed too much", required)
		}
	}
}

func TestHarnessCreatesEventBus(t *testing.T) {
	h := newTestHarness(t)
	if h.EventBus() == nil {
		t.Fatal("EventBus() should not be nil")
	}
}

func TestHarnessEmitsTurnEventsOnRunTurn(t *testing.T) {
	h := newTestHarness(t)

	ch := h.EventBus().Subscribe(50)

	if _, err := h.RunTurn(context.Background(), "hello"); err != nil {
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

func TestHarnessRoleModelsFallBackToMain(t *testing.T) {
	var built []string
	factory := func(m common.ModelDefinition) (common.LLM, error) {
		built = append(built, m.Name)
		return &stubLLM{}, nil
	}
	_, err := New(testModel, WithAgentBuilder(stubBrain), WithLLMFactory(factory), WithSystemPrompt("test"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	for _, name := range built {
		if name != testModel.Name {
			t.Errorf("built LLM for model %q, want only %q (fallback to main)", name, testModel.Name)
		}
	}
}

func TestHarnessDefaultAgentBuilder(t *testing.T) {
	h, err := New(testModel, WithLLMFactory(stubFactory), WithSystemPrompt("test"))
	if err != nil {
		t.Fatalf("New() without WithAgentBuilder error: %v", err)
	}
	if h == nil {
		t.Fatal("New() returned nil harness")
	}
}

func hasTool(h *Harness, name string) bool {
	for _, def := range h.ToolDefinitions() {
		if def.Name() == name {
			return true
		}
	}
	return false
}

func TestHarnessRegistersREPLToolByDefault(t *testing.T) {
	h := newTestHarness(t)
	defer h.Shutdown()
	if !hasTool(h, "repl") {
		t.Error("repl tool should be registered by default")
	}
}

func TestHarnessBlackboardDisabled(t *testing.T) {
	h := newTestHarness(t, WithBlackboardDisabled())
	defer h.Shutdown()
	if hasTool(h, "repl") {
		t.Error("repl tool should not be registered when blackboard is disabled")
	}
}
