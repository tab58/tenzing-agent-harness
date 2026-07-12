package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/prompts"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"

	"github.com/tab58/llm-providers/common"
)

type stubAgent struct{}

func (s *stubAgent) GetCurrentModel() string                         { return "stub-model" }
func (s *stubAgent) UpdateToolDefinitions(_ []common.ToolDefinition) {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)              {}
func (s *stubAgent) UpdateStreamCallback(_ func(string))             {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string))           {}
func (s *stubAgent) SetTodoProvider(_ func() string)                 {}

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

// newTestHarness builds a harness with stubbed LLMs and brain. HOME is
// redirected so the memory sweep and persistence never touch real dirs.
func newTestHarness(t *testing.T, opts ...HarnessOption) *Harness {
	t.Helper()
	redirectHome(t)
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

// Regression: without WithSystemPrompt, the runner logged the default prompt
// but the agent was built with "" — every main-agent request went to the LLM
// with an empty system prompt.
func TestMainAgentBuiltWithResolvedSystemPrompt(t *testing.T) {
	tests := []struct {
		name string
		opts []HarnessOption
		want string
	}{
		{"explicit prompt", []HarnessOption{WithSystemPrompt("custom prompt")}, "custom prompt"},
		{"default prompt when unset", nil, prompts.DefaultSystemPrompt()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redirectHome(t)
			var captured string
			builder := func(_ common.LLM, sp string) (runner.Agent, error) {
				captured = sp
				return &stubAgent{}, nil
			}
			h, err := New(testModel, append([]HarnessOption{
				WithAgentBuilder(builder),
				WithLLMFactory(stubFactory),
			}, tt.opts...)...)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			if captured == "" {
				t.Fatal("agent built with empty system prompt")
			}
			if captured != tt.want {
				t.Errorf("agent system prompt = %q, want %q", captured, tt.want)
			}
			// The runner's copy (logging/accessor) must match what the agent got.
			if h.SystemPrompt() != captured {
				t.Errorf("runner prompt %q != agent prompt %q", h.SystemPrompt(), captured)
			}
		})
	}
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
	redirectHome(t)
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
	redirectHome(t)
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

func TestWithConversationIDSetsRunnerID(t *testing.T) {
	redirectHome(t)
	configDir, _ := memoryDirs()
	if err := os.WriteFile(filepath.Join(configDir, ".agent_memory-20260710-0900-cafe0001.md"),
		[]byte("# Agent Memory\n\nresume state marker\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Default builder path (no WithAgentBuilder): construction must succeed
	// with a memory file present and adopt the supplied conversation ID.
	h, err := New(testModel, WithLLMFactory(stubFactory), WithConversationID("cafe0001"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer h.Shutdown()
	if h.ConversationID() != "cafe0001" {
		t.Fatalf("ConversationID = %q, want cafe0001", h.ConversationID())
	}
}

func TestCompressionEventPersistsMemory(t *testing.T) {
	redirectHome(t)
	h := newTestHarness(t)
	defer h.Shutdown()

	h.EventBus().Emit(events.ContextCompressedEvent{
		BaseEvent: events.NewBaseEvent(events.EventContextCompressed, h.ConversationID()),
		Summary:   "persisted by subscriber",
	})
	configDir, _ := memoryDirs()
	deadline := time.Now().Add(2 * time.Second)
	for {
		matches, _ := filepath.Glob(filepath.Join(configDir, ".agent_memory-*-"+h.ConversationID()+".md"))
		if len(matches) == 1 {
			data, _ := os.ReadFile(matches[0])
			if strings.Contains(string(data), "persisted by subscriber") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber never persisted the compression event")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestChildCompressionGoesToCache(t *testing.T) {
	redirectHome(t)
	h := newTestHarness(t)
	defer h.Shutdown()

	childID := h.ConversationID() + "_deadbeef"
	h.EventBus().Emit(events.ContextCompressedEvent{
		BaseEvent: events.NewBaseEvent(events.EventContextCompressed, childID),
		Summary:   "child summary",
	})
	_, cacheDir := memoryDirs()
	deadline := time.Now().Add(2 * time.Second)
	for {
		matches, _ := filepath.Glob(filepath.Join(cacheDir, ".agent_memory-*-"+childID+".md"))
		if len(matches) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child compression not persisted to cache dir")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
