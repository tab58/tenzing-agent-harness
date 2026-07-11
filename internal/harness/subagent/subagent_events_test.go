package subagent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tab58/llm-providers/common"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
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

func TestSubAgentFactoryEmitsLifecycleEvents(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()

	factory := NewSubAgentFactory(SubAgentFactoryConfig{
		AgentLLM: &stubLLM{},
		AgentBuilder: func(_ common.LLM, _ string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
		MaxDepth: 1,
		Cwd:      dir,
		Emitter:  collector,
		ParentID: "deadbeef",
	})

	result, err := factory.SpawnAgent(context.Background(), "do something", "")
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}

	started := collector.byType(events.EventSubagentStarted)
	if len(started) != 1 {
		t.Fatalf("expected 1 SubagentStarted, got %d", len(started))
	}
	ev := started[0].(events.SubagentStartedEvent)
	if !strings.HasPrefix(ev.AgentID, "deadbeef_") {
		t.Errorf("agent ID %q not derived from parent ID", ev.AgentID)
	}
	if ev.RunnerID != ev.AgentID {
		t.Errorf("runner ID %q != agent ID %q; they must be unified", ev.RunnerID, ev.AgentID)
	}
	stopped := collector.byType(events.EventSubagentStopped)
	if len(stopped) != 1 {
		t.Fatalf("expected 1 SubagentStopped, got %d", len(stopped))
	}

	// The child runner shares the factory's emitter, so its own loop events
	// must land on the same collector (this is what surfaces sub-agent
	// activity in the UI).
	if got := collector.byType(events.EventLoopStarted); len(got) != 1 {
		t.Fatalf("expected 1 child LoopStarted, got %d", len(got))
	}
	if got := collector.byType(events.EventLoopStopped); len(got) != 1 {
		t.Fatalf("expected 1 child LoopStopped, got %d", len(got))
	}
}
