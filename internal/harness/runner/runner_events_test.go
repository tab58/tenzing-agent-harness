package runner

import (
	"context"
	"sync"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/harness/tools/tooldef"

	"github.com/tab58/llm-providers/common"
)

type eventCollector struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *eventCollector) Emit(ev events.Event) {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.mu.Unlock()
}

func (c *eventCollector) byType(t events.EventType) []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []events.Event
	for _, ev := range c.events {
		if ev.Type() == t {
			out = append(out, ev)
		}
	}
	return out
}

type minimalAgent struct {
	mu    sync.Mutex
	steps []ReasoningResult
	idx   int
}

func (a *minimalAgent) GetCurrentModel() string                                         { return "" }
func (a *minimalAgent) UpdateToolDefinitions(_ []common.ToolDefinition)                 {}
func (a *minimalAgent) UpdateSkillMap(_ map[string]string)                              {}
func (a *minimalAgent) UpdateOffloadFn(_ func(context.Context, string) (string, error)) {}
func (a *minimalAgent) UpdateStreamCallback(_ func(string))                             {}
func (a *minimalAgent) UpdateThinkingCallback(_ func(string))                           {}
func (a *minimalAgent) SetTodoProvider(_ func() string)                                 {}

func (a *minimalAgent) DoReasoning(_ context.Context, _ []string, _ []string) (ReasoningResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.steps[a.idx]
	a.idx++
	return r, nil
}

func TestRunnerEmitsTurnAndLoopEvents(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()

	agent := &minimalAgent{steps: []ReasoningResult{
		{FinalAnswer: "done"},
	}}

	r, err := NewAgentRunner(
		agent,
		WithEmitter(collector),
		WithToolRegistry(tools.NewRegistry("")),
		WithSkillsRegistry(skills.NewRegistry()),
		WithTodoFile(todo.NewTodoFile(dir)),
		WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatal(err)
	}

	answer, err := r.RunLoop(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "done" {
		t.Errorf("answer = %q, want %q", answer, "done")
	}

	if len(collector.byType(events.EventTurnStarted)) != 1 {
		t.Errorf("expected 1 TurnStarted, got %d", len(collector.byType(events.EventTurnStarted)))
	}
	if len(collector.byType(events.EventLoopStarted)) != 1 {
		t.Errorf("expected 1 LoopStarted, got %d", len(collector.byType(events.EventLoopStarted)))
	}
	if len(collector.byType(events.EventReasoningStarted)) != 1 {
		t.Errorf("expected 1 ReasoningStarted, got %d", len(collector.byType(events.EventReasoningStarted)))
	}
	if len(collector.byType(events.EventReasoningFinished)) != 1 {
		t.Errorf("expected 1 ReasoningFinished, got %d", len(collector.byType(events.EventReasoningFinished)))
	}
	if len(collector.byType(events.EventLoopStopped)) != 1 {
		t.Errorf("expected 1 LoopStopped, got %d", len(collector.byType(events.EventLoopStopped)))
	}
	if len(collector.byType(events.EventTurnCompleted)) != 1 {
		t.Errorf("expected 1 TurnCompleted, got %d", len(collector.byType(events.EventTurnCompleted)))
	}
}

func TestRunnerEmitsToolEvents(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()

	registry := tools.NewRegistry("")
	registry.Register(&echoTool{})

	agent := &minimalAgent{steps: []ReasoningResult{
		{ToolCall: &tooldef.ToolCall{ID: "1", Name: "echo", Input: `{"text":"hi"}`}},
		{FinalAnswer: "done"},
	}}

	r, err := NewAgentRunner(
		agent,
		WithEmitter(collector),
		WithToolRegistry(registry),
		WithSkillsRegistry(skills.NewRegistry()),
		WithTodoFile(todo.NewTodoFile(dir)),
		WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.RunLoop(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}

	if len(collector.byType(events.EventToolExecutionStarted)) != 1 {
		t.Errorf("expected 1 ToolExecutionStarted, got %d", len(collector.byType(events.EventToolExecutionStarted)))
	}
	if len(collector.byType(events.EventToolSucceeded)) != 1 {
		t.Errorf("expected 1 ToolSucceeded, got %d", len(collector.byType(events.EventToolSucceeded)))
	}
	if len(collector.byType(events.EventToolExecutionFinished)) != 1 {
		t.Errorf("expected 1 ToolExecutionFinished, got %d", len(collector.byType(events.EventToolExecutionFinished)))
	}
}

type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) Schema() tooldef.Schema {
	return tooldef.Schema{Properties: map[string]tooldef.SchemaProperty{"text": {Type: tooldef.JsonTypeString}}}
}
func (e *echoTool) Execute(_ context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	return tooldef.NewToolResult("echo: " + exctx.Arguments[0]), nil
}
