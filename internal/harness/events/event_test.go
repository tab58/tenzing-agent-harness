package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBaseEventImplementsEvent(t *testing.T) {
	be := NewBaseEvent(EventLoopStarted, "runner-1")

	if be.Type() != EventLoopStarted {
		t.Errorf("Type() = %q, want %q", be.Type(), EventLoopStarted)
	}
	if be.Timestamp().IsZero() {
		t.Error("Timestamp() should not be zero")
	}
	if time.Since(be.Timestamp()) > time.Second {
		t.Error("Timestamp() should be recent")
	}
}

func TestBaseEventJSONRoundTrip(t *testing.T) {
	be := NewBaseEvent(EventToolSucceeded, "r-abc")

	data, err := json.Marshal(be)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded BaseEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.EventType != EventToolSucceeded {
		t.Errorf("EventType = %q, want %q", decoded.EventType, EventToolSucceeded)
	}
	if decoded.RunnerID != "r-abc" {
		t.Errorf("RunnerID = %q, want %q", decoded.RunnerID, "r-abc")
	}
}

func TestConcreteEventImplementsEvent(t *testing.T) {
	ev := LoopStartedEvent{
		BaseEvent: NewBaseEvent(EventLoopStarted, "r1"),
		Input:     "hello",
	}

	var _ Event = ev
	if ev.Type() != EventLoopStarted {
		t.Errorf("Type() = %q, want %q", ev.Type(), EventLoopStarted)
	}
}

func TestConcreteEventJSON(t *testing.T) {
	ev := ToolSucceededEvent{
		BaseEvent: NewBaseEvent(EventToolSucceeded, "r1"),
		ToolName:  "bash",
		Input:     "ls",
		Output:    "file.go",
		Duration:  42 * time.Millisecond,
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	json.Unmarshal(data, &m)
	if m["tool_name"] != "bash" {
		t.Errorf("tool_name = %v, want bash", m["tool_name"])
	}
	if m["type"] != string(EventToolSucceeded) {
		t.Errorf("type = %v, want %s", m["type"], EventToolSucceeded)
	}
}
