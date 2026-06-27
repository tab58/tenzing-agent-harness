package events

import (
	"sync"
	"testing"
	"time"
)

func TestEventBusImplementsEmitter(t *testing.T) {
	var _ Emitter = NewEventBus()
}

func TestSubscribeReceivesEmittedEvents(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ch := bus.Subscribe(10)
	ev := LoopStartedEvent{
		BaseEvent: NewBaseEvent(EventLoopStarted, "r1"),
		Input:     "hello",
	}
	bus.Emit(ev)

	select {
	case got := <-ch:
		if got.Type() != EventLoopStarted {
			t.Errorf("Type() = %q, want %q", got.Type(), EventLoopStarted)
		}
		lse, ok := got.(LoopStartedEvent)
		if !ok {
			t.Fatalf("expected LoopStartedEvent, got %T", got)
		}
		if lse.Input != "hello" {
			t.Errorf("Input = %q, want %q", lse.Input, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ch1 := bus.Subscribe(10)
	ch2 := bus.Subscribe(10)

	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Type() != EventLoopStarted {
				t.Errorf("Type() = %q, want %q", got.Type(), EventLoopStarted)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestEmitDropsWhenBufferFull(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ch := bus.Subscribe(1)

	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})
	bus.Emit(LoopStoppedEvent{BaseEvent: NewBaseEvent(EventLoopStopped, "r1")})

	got := <-ch
	if got.Type() != EventLoopStarted {
		t.Errorf("expected first event, got %q", got.Type())
	}

	select {
	case extra := <-ch:
		t.Errorf("expected no second event, got %q", extra.Type())
	default:
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ch := bus.Subscribe(10)
	bus.Unsubscribe(ch)

	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after Unsubscribe")
		}
	default:
	}
}

func TestCloseClosesAllSubscribers(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe(10)
	ch2 := bus.Subscribe(10)

	bus.Close()

	for _, ch := range []<-chan Event{ch1, ch2} {
		_, ok := <-ch
		if ok {
			t.Error("channel should be closed after Close")
		}
	}
}

func TestEmitAfterCloseIsNoop(t *testing.T) {
	bus := NewEventBus()
	bus.Close()

	// should not panic
	bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})
}

func TestConcurrentEmit(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ch := bus.Subscribe(100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Emit(LoopStartedEvent{BaseEvent: NewBaseEvent(EventLoopStarted, "r1")})
		}()
	}
	wg.Wait()

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 50 {
		t.Errorf("received %d events, want 50", count)
	}
}
