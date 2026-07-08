package nexus

import (
	"testing"
)

func testChannelConfig(name string) ChannelConfig {
	return ChannelConfig{
		Name:         name,
		Type:         TypeWebhook,
		ErrorPattern: DefaultErrorPattern,
		BufferSize:   10,
	}
}

func TestChannelIngest(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantError bool
	}{
		{"plain line", "all good here", false},
		{"error lowercase", "an error occurred", true},
		{"ERROR uppercase", "ERROR: boom", true},
		{"panic", "panic: nil deref", true},
		{"fatal", "FATAL shutdown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fired []Entry
			ch, err := newChannel(testChannelConfig("t"), func(e Entry) { fired = append(fired, e) })
			if err != nil {
				t.Fatal(err)
			}
			e := ch.Ingest(tt.text)
			if e.IsError != tt.wantError {
				t.Errorf("IsError = %v, want %v", e.IsError, tt.wantError)
			}
			if tt.wantError && len(fired) != 1 {
				t.Errorf("onError fired %d times, want 1", len(fired))
			}
			if !tt.wantError && len(fired) != 0 {
				t.Errorf("onError fired %d times, want 0", len(fired))
			}
		})
	}
}

func TestChannelIngestNilOnError(t *testing.T) {
	ch, err := newChannel(testChannelConfig("t"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// must not panic
	ch.Ingest("error: boom")
}

func TestChannelBadPattern(t *testing.T) {
	cfg := testChannelConfig("t")
	cfg.ErrorPattern = "(unclosed"
	if _, err := newChannel(cfg, nil); err == nil {
		t.Fatal("want error for bad pattern")
	}
}

func TestChannelStatus(t *testing.T) {
	ch, err := newChannel(testChannelConfig("t"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Status() != "stopped" {
		t.Errorf("initial status = %q, want stopped", ch.Status())
	}
	ch.setStatus("running")
	if ch.Status() != "running" {
		t.Errorf("status = %q, want running", ch.Status())
	}
}
