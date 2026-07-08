package nexus

import (
	"sync"
	"testing"
	"time"
)

// fakeWake records wake calls and returns a scripted busy/idle answer.
type fakeWake struct {
	mu    sync.Mutex
	calls [][]string
	busy  bool
}

func (f *fakeWake) wake(chs []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), chs...))
	return !f.busy
}

func (f *fakeWake) setBusy(b bool) {
	f.mu.Lock()
	f.busy = b
	f.mu.Unlock()
}

func (f *fakeWake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeWake) lastCall() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func TestTriggerFiresWhenIdle(t *testing.T) {
	fw := &fakeWake{}
	tr := NewTrigger(time.Hour, fw.wake)

	tr.Notify("api-logs")
	if fw.callCount() != 1 {
		t.Fatalf("wake calls = %d, want 1", fw.callCount())
	}
	if got := fw.lastCall(); len(got) != 1 || got[0] != "api-logs" {
		t.Errorf("wake channels = %v, want [api-logs]", got)
	}
}

func TestTriggerCooldownSuppressesRepeat(t *testing.T) {
	fw := &fakeWake{}
	tr := NewTrigger(time.Hour, fw.wake)

	tr.Notify("api-logs")
	tr.Notify("api-logs") // within cooldown
	tr.Notify("api-logs")
	if fw.callCount() != 1 {
		t.Errorf("wake calls = %d, want 1 (cooldown must suppress)", fw.callCount())
	}
}

func TestTriggerCooldownExpires(t *testing.T) {
	fw := &fakeWake{}
	tr := NewTrigger(20*time.Millisecond, fw.wake)

	tr.Notify("api-logs")
	time.Sleep(30 * time.Millisecond)
	tr.Notify("api-logs")
	if fw.callCount() != 2 {
		t.Errorf("wake calls = %d, want 2 after cooldown expiry", fw.callCount())
	}
}

func TestTriggerBusyQueuesThenFlushes(t *testing.T) {
	fw := &fakeWake{}
	fw.setBusy(true)
	tr := NewTrigger(time.Hour, fw.wake)

	tr.Notify("api-logs")
	tr.Notify("docker-web") // second channel while busy
	if fw.callCount() < 1 {
		t.Fatal("wake should have been attempted")
	}

	fw.setBusy(false)
	tr.TurnEnded()

	got := fw.lastCall()
	if len(got) != 2 {
		t.Fatalf("flushed channels = %v, want both api-logs and docker-web", got)
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen["api-logs"] || !seen["docker-web"] {
		t.Errorf("flushed channels = %v", got)
	}

	// pending cleared: another TurnEnded is a no-op
	n := fw.callCount()
	tr.TurnEnded()
	if fw.callCount() != n {
		t.Error("TurnEnded with no pending must not wake")
	}
}
