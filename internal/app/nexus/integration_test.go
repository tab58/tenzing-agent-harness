package nexus

import (
	"testing"
	"time"
)

func TestNexusTriggerEndToEnd(t *testing.T) {
	var calls [][]string
	busy := false
	trig := NewTrigger(time.Hour, func(chs []string) bool {
		calls = append(calls, append([]string(nil), chs...))
		return !busy
	})

	n, err := New(Config{Channels: []ChannelConfig{
		{Name: "a", Type: TypeWebhook, ErrorPattern: DefaultErrorPattern, BufferSize: 10},
		{Name: "b", Type: TypeWebhook, ErrorPattern: DefaultErrorPattern, BufferSize: 10},
	}}, nil, trig.Notify)
	if err != nil {
		t.Fatal(err)
	}

	// non-error line: no wake
	n.Ingest("a", "all quiet")
	if len(calls) != 0 {
		t.Fatalf("wake called for non-error line: %v", calls)
	}

	// error line: wake fires with the channel name
	n.Ingest("a", "error: boom")
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "a" {
		t.Fatalf("calls = %v, want [[a]]", calls)
	}

	// second error on same channel within cooldown: suppressed, but still buffered
	n.Ingest("a", "error: again")
	if len(calls) != 1 {
		t.Fatalf("cooldown did not suppress: %v", calls)
	}
	errs, err := n.Read("a", 10, true)
	if err != nil || len(errs) != 2 {
		t.Fatalf("both errors should be buffered, got %d (err %v)", len(errs), err)
	}

	// busy agent: error on another channel stays pending, flushes on TurnEnded
	busy = true
	n.Ingest("b", "panic: nil deref")
	if len(calls) != 2 {
		t.Fatalf("busy wake attempt missing: %v", calls)
	}
	busy = false
	trig.TurnEnded()
	if len(calls) != 3 || len(calls[2]) != 1 || calls[2][0] != "b" {
		t.Fatalf("pending flush wrong: %v", calls)
	}
}
