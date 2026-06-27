package taskgraph

import (
	"encoding/json"
	"sync"
	"testing"

	"tenzing-agent/internal/harness/events"
)

type eventCollector struct {
	mu   sync.Mutex
	evts []events.Event
}

func (c *eventCollector) Emit(ev events.Event) {
	c.mu.Lock()
	c.evts = append(c.evts, ev)
	c.mu.Unlock()
}

func (c *eventCollector) byType(t events.EventType) []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []events.Event
	for _, ev := range c.evts {
		if ev.Type() == t {
			out = append(out, ev)
		}
	}
	return out
}

func TestTaskGraphEmitsTaskCreated(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()
	g := NewTaskGraph(dir)
	g.SetEmitter(collector)

	_, err := g.CreateTask("build feature", nil, TaskPriorityMedium)
	if err != nil {
		t.Fatal(err)
	}

	created := collector.byType(events.EventTaskCreated)
	if len(created) != 1 {
		t.Fatalf("expected 1 TaskCreated, got %d", len(created))
	}
	ev := created[0].(events.TaskCreatedEvent)
	if ev.Description != "build feature" {
		t.Errorf("Description = %q, want %q", ev.Description, "build feature")
	}
}

func TestTaskGraphEmitsTaskCompleted(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()
	g := NewTaskGraph(dir)
	g.SetEmitter(collector)

	raw, err := g.CreateTask("task one", nil, TaskPriorityMedium)
	if err != nil {
		t.Fatal(err)
	}

	// extract task ID from JSON
	var task Task
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}

	err = g.UpdateTask(task.ID, "done", "finished")
	if err != nil {
		t.Fatal(err)
	}

	completed := collector.byType(events.EventTaskCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 TaskCompleted, got %d", len(completed))
	}
	ev := completed[0].(events.TaskCompletedEvent)
	if ev.TaskID != task.ID {
		t.Errorf("TaskID = %q, want %q", ev.TaskID, task.ID)
	}
}

func TestTaskGraphNoEventsWithNilEmitter(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	// should not panic
	_, err := g.CreateTask("no emitter", nil, TaskPriorityMedium)
	if err != nil {
		t.Fatal(err)
	}
}
