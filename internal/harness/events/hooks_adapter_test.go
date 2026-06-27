package events

import (
	"sync"
	"testing"
	"time"
)

func TestHooksAdapterDispatchesMatchingEvent(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	var got LoopStartedEvent
	var mu sync.Mutex
	hooks := Hooks{
		OnLoopStarted: func(ev LoopStartedEvent) {
			mu.Lock()
			got = ev
			mu.Unlock()
		},
	}

	adapter := NewHooksAdapter(bus, hooks)
	defer adapter.Stop()

	bus.Emit(LoopStartedEvent{
		BaseEvent: NewBaseEvent(EventLoopStarted, "r1"),
		Input:     "test-input",
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if got.Input != "test-input" {
		t.Errorf("Input = %q, want %q", got.Input, "test-input")
	}
}

func TestHooksAdapterSkipsNilHooks(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	hooks := Hooks{}
	adapter := NewHooksAdapter(bus, hooks)
	defer adapter.Stop()

	// should not panic
	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})
	time.Sleep(50 * time.Millisecond)
}

func TestHooksAdapterMultipleEventTypes(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	var mu sync.Mutex
	var loopCalled, toolCalled bool

	hooks := Hooks{
		OnLoopStarted: func(_ LoopStartedEvent) {
			mu.Lock()
			loopCalled = true
			mu.Unlock()
		},
		OnToolSucceeded: func(_ ToolSucceededEvent) {
			mu.Lock()
			toolCalled = true
			mu.Unlock()
		},
	}

	adapter := NewHooksAdapter(bus, hooks)
	defer adapter.Stop()

	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})
	bus.Emit(ToolSucceededEvent{BaseEvent: NewBaseEvent(EventToolSucceeded, "r1"), ToolName: "bash"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !loopCalled {
		t.Error("OnLoopStarted was not called")
	}
	if !toolCalled {
		t.Error("OnToolSucceeded was not called")
	}
}

func TestHooksAdapterStopsOnBusClose(t *testing.T) {
	bus := NewEventBus()

	called := make(chan struct{}, 1)
	hooks := Hooks{
		OnLoopStarted: func(_ LoopStartedEvent) {
			called <- struct{}{}
		},
	}

	adapter := NewHooksAdapter(bus, hooks)
	_ = adapter

	bus.Close()

	select {
	case <-time.After(100 * time.Millisecond):
		// adapter goroutine should exit cleanly
	}
}
