package subagent

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/tab58/tenzing-agent-harness/internal/harness/blackboard"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"

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

func (s *stubAgent) GetCurrentModel() string                         { return "stub" }
func (s *stubAgent) UpdateToolDefinitions(_ []common.ToolDefinition) {}
func (s *stubAgent) UpdateSkillMap(_ map[string]string)              {}
func (s *stubAgent) UpdateStreamCallback(_ func(string))             {}
func (s *stubAgent) UpdateThinkingCallback(_ func(string))           {}
func (s *stubAgent) SetTodoProvider(_ func() string)                 {}

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

	registry := factory.buildChildToolRegistry("a0")
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

	registry := factory.buildChildToolRegistry("a0")
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

func TestSubAgentFactoryImmutability(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      2,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
	})

	depthBefore := factory.currentDepth
	_ = factory.buildChildToolRegistry("a0")

	if factory.currentDepth != depthBefore {
		t.Fatalf("factory currentDepth mutated: was %d, now %d", depthBefore, factory.currentDepth)
	}
}

// fixedAnswerAgent returns a canned final answer of any size.
type fixedAnswerAgent struct{ answer string }

func (s *fixedAnswerAgent) GetCurrentModel() string                         { return "stub" }
func (s *fixedAnswerAgent) UpdateToolDefinitions(_ []common.ToolDefinition) {}
func (s *fixedAnswerAgent) UpdateSkillMap(_ map[string]string)              {}
func (s *fixedAnswerAgent) UpdateStreamCallback(_ func(string))             {}
func (s *fixedAnswerAgent) UpdateThinkingCallback(_ func(string))           {}
func (s *fixedAnswerAgent) SetTodoProvider(_ func() string)                 {}
func (s *fixedAnswerAgent) DoReasoning(_ context.Context, _ []string, _ []string) (runner.ReasoningResult, error) {
	return runner.ReasoningResult{FinalAnswer: s.answer}, nil
}

func newTestBlackboard(t *testing.T) *blackboard.Blackboard {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found on PATH")
	}
	bb := blackboard.New(blackboard.Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { _ = bb.Close() })
	return bb
}

func factoryWithAnswer(t *testing.T, bb *blackboard.Blackboard, answer string) *SubAgentFactory {
	t.Helper()
	return NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:      &stubLLM{},
		MaxDepth:      1,
		MaxIterations: 5,
		Cwd:           t.TempDir(),
		Blackboard:    bb,
		AgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
			return &fixedAnswerAgent{answer: answer}, nil
		},
	})
}

func TestSpawnAgentSmallResultReturnedInline(t *testing.T) {
	bb := newTestBlackboard(t)
	factory := factoryWithAnswer(t, bb, "short answer")

	result, err := factory.SpawnAgent(context.Background(), "task", "")
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if !strings.HasSuffix(result, "short answer") {
		t.Errorf("small result should end with the verbatim answer, got %q", result)
	}
	m := regexp.MustCompile(`^Sub-agent ([0-9a-f_]+) completed \(blackboard slot bb\['([0-9a-f_]+)'\]\)\. `).FindStringSubmatch(result)
	if m == nil {
		t.Fatalf("inline result missing slot prefix: %q", result)
	}
	if m[1] != m[2] {
		t.Errorf("agent id %q != slot %q in prefix", m[1], m[2])
	}
}

func TestSpawnAgentLargeResultDepositedWithPreview(t *testing.T) {
	bb := newTestBlackboard(t)
	long := strings.Repeat("HEAD", 500) + strings.Repeat("TAIL", 500) // 4000 chars
	factory := factoryWithAnswer(t, bb, long)

	result, err := factory.SpawnAgent(context.Background(), "task", "")
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if len(result) >= len(long) {
		t.Errorf("result not truncated: %d chars", len(result))
	}
	if !strings.Contains(result, "4000 chars") {
		t.Errorf("preview missing size: %q", result)
	}

	m := regexp.MustCompile(`bb\["([0-9a-f_]+)"\]`).FindStringSubmatch(result)
	if m == nil {
		t.Fatalf("preview does not reference a bb slot: %q", result)
	}
	out, err := bb.Execute(context.Background(), "main",
		"print(len(bb['"+m[1]+"']['result']))")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if strings.TrimSpace(out) != "4000" {
		t.Errorf("deposited length = %q, want 4000", out)
	}
}

func TestSpawnAgentNilBlackboardReturnsFullResult(t *testing.T) {
	long := strings.Repeat("x", 5000)
	factory := factoryWithAnswer(t, nil, long)

	result, err := factory.SpawnAgent(context.Background(), "task", "")
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if result != long {
		t.Errorf("nil blackboard must inline full result, got %d chars", len(result))
	}
}

func TestChildRegistryHasREPLToolWhenBlackboardSet(t *testing.T) {
	bb := newTestBlackboard(t)
	factory := factoryWithAnswer(t, bb, "x")

	registry := factory.buildChildToolRegistry("a99")
	found := false
	for _, def := range registry.Definitions() {
		if def.Name() == "repl" {
			found = true
			break
		}
	}
	if !found {
		t.Error("child registry missing repl tool when blackboard is set")
	}
}

// Regression: a sub-agent that doesn't know the working directory guesses
// paths (observed: /home/user/repo, then a filesystem-wide find).
func TestSubAgentSystemPromptIncludesWorkingDir(t *testing.T) {
	cwd := t.TempDir()
	var capturedPrompt string
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM: &stubLLM{},
		Cwd:      cwd,
		AgentBuilder: func(llm common.LLM, sp string) (runner.Agent, error) {
			capturedPrompt = sp
			return &stubAgent{}, nil
		},
	})

	if _, err := factory.SpawnAgent(context.Background(), "task", ""); err != nil {
		t.Fatalf("SpawnAgent error: %v", err)
	}
	if !strings.Contains(capturedPrompt, cwd) {
		t.Fatalf("system prompt missing working directory %q:\n%s", cwd, capturedPrompt)
	}
}

func TestChildRegistryHasNoREPLToolWithoutBlackboard(t *testing.T) {
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM: &stubLLM{},
		Cwd:      t.TempDir(),
	})
	for _, def := range factory.buildChildToolRegistry("a99").Definitions() {
		if def.Name() == "repl" {
			t.Error("child registry has repl tool despite nil blackboard")
		}
	}
}

// recordingAgent captures the inputs RunLoop feeds into reasoning.
type recordingAgent struct {
	stubAgent
	inputs []string
}

func (r *recordingAgent) DoReasoning(_ context.Context, inputs []string, _ []string) (runner.ReasoningResult, error) {
	r.inputs = append(r.inputs, inputs...)
	return runner.ReasoningResult{FinalAnswer: "done"}, nil
}

// Regression: sub-agents obeyed task-prompt instructions to deposit under
// invented slot names. Every task is inoculated with the canonical slot.
func TestSpawnAgentTaskInoculatedWithOwnSlot(t *testing.T) {
	bb := newTestBlackboard(t)
	rec := &recordingAgent{}
	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM:   &stubLLM{},
		MaxDepth:   1,
		Cwd:        t.TempDir(),
		Blackboard: bb,
		ParentID:   "beef0000",
		AgentBuilder: func(_ common.LLM, _ string) (runner.Agent, error) {
			return rec, nil
		},
	})

	if _, err := factory.SpawnAgent(context.Background(), "dump files into bb['agents_md']['result']", ""); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if len(rec.inputs) == 0 {
		t.Fatal("agent received no input")
	}
	input := rec.inputs[0]
	if !strings.Contains(input, "Deposit results ONLY in bb['beef0000_") {
		t.Fatalf("task not inoculated with canonical slot:\n%s", input)
	}
}

// Short results must ALSO be deposited: the factory's completion message
// names the canonical slot, so that slot must actually exist.
func TestSpawnAgentSmallResultAlsoDeposited(t *testing.T) {
	bb := newTestBlackboard(t)
	factory := factoryWithAnswer(t, bb, "short answer")

	result, err := factory.SpawnAgent(context.Background(), "task", "")
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	m := regexp.MustCompile(`bb\['([0-9a-f_]+)'\]`).FindStringSubmatch(result)
	if m == nil {
		t.Fatalf("no slot in result: %q", result)
	}
	out, err := bb.Execute(context.Background(), "main", "print(bb['"+m[1]+"']['result'])")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "short answer") {
		t.Fatalf("canonical slot empty for short result: %q", out)
	}
}
