package subagent

import (
	"context"
	"sync"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/provider"
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
		RLMModel: &stubLLM{},
		AgentBuilder: func(_ provider.LLM, _ string) (runner.Agent, error) {
			return &stubAgent{}, nil
		},
		MaxDepth: 1,
		Cwd:      dir,
		Emitter:  collector,
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
	stopped := collector.byType(events.EventSubagentStopped)
	if len(stopped) != 1 {
		t.Fatalf("expected 1 SubagentStopped, got %d", len(stopped))
	}
}
