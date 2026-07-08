package nexus

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type statusCollector struct {
	mu       sync.Mutex
	statuses []string
}

func (sc *statusCollector) set(s string) {
	sc.mu.Lock()
	sc.statuses = append(sc.statuses, s)
	sc.mu.Unlock()
}

func (sc *statusCollector) snapshot() []string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return append([]string(nil), sc.statuses...)
}

func TestCommandCapturesStdoutAndStderr(t *testing.T) {
	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		runCommand(ctx,
			[]string{"sh", "-c", "echo out-line; echo err-line 1>&2; sleep 60"},
			10*time.Millisecond, 100*time.Millisecond,
			lc.ingest, func(string) {})
		close(done)
	}()

	if !waitFor(t, 2*time.Second, func() bool { return len(lc.snapshot()) >= 2 }) {
		t.Fatalf("lines = %v, want both out-line and err-line", lc.snapshot())
	}
	got := lc.snapshot()
	seen := map[string]bool{}
	for _, l := range got {
		seen[l] = true
	}
	if !seen["out-line"] || !seen["err-line"] {
		t.Errorf("lines = %v, want out-line and err-line", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCommand did not return after cancel")
	}
}

func TestCommandRestartsOnExit(t *testing.T) {
	lc := &lineCollector{}
	sc := &statusCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runCommand(ctx,
		[]string{"sh", "-c", "echo tick"},
		10*time.Millisecond, 50*time.Millisecond,
		lc.ingest, sc.set)

	// command exits immediately after echoing; restart should produce
	// multiple ticks
	if !waitFor(t, 3*time.Second, func() bool { return len(lc.snapshot()) >= 3 }) {
		t.Fatalf("got %d ticks, want >= 3 (restart not happening)", len(lc.snapshot()))
	}

	cancel()
	if !waitFor(t, 2*time.Second, func() bool {
		s := sc.snapshot()
		return len(s) > 0 && s[len(s)-1] == "stopped"
	}) {
		t.Fatalf("final status not stopped: %v", sc.snapshot())
	}

	// restarting must appear between runs
	foundRestarting := false
	for _, s := range sc.snapshot() {
		if s == "restarting" {
			foundRestarting = true
		}
	}
	if !foundRestarting {
		t.Errorf("statuses = %v, want a restarting entry", sc.snapshot())
	}
}

func TestCommandBadBinaryKeepsRetrying(t *testing.T) {
	sc := &statusCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		runCommand(ctx,
			[]string{"/nonexistent-binary-xyz"},
			10*time.Millisecond, 50*time.Millisecond,
			func(string) {}, sc.set)
		close(done)
	}()

	// must not panic or return; it keeps retrying until cancel
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCommand did not return after cancel")
	}
}

func TestCommandKillsProcessGroup(t *testing.T) {
	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		// grandchild sleeps in the background; sh echoes its PID and waits
		runCommand(ctx,
			[]string{"sh", "-c", "sleep 60 & echo $!; wait"},
			10*time.Millisecond, 100*time.Millisecond,
			lc.ingest, func(string) {})
		close(done)
	}()

	if !waitFor(t, 2*time.Second, func() bool { return len(lc.snapshot()) >= 1 }) {
		t.Fatal("grandchild pid line never arrived")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lc.snapshot()[0]))
	if err != nil {
		t.Fatalf("bad pid line %q: %v", lc.snapshot()[0], err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCommand did not return after cancel")
	}

	// grandchild must die with the group; signal 0 probes existence
	if !waitFor(t, 2*time.Second, func() bool {
		return syscall.Kill(pid, 0) != nil
	}) {
		syscall.Kill(pid, syscall.SIGKILL) // cleanup so the test doesn't leak it
		t.Fatalf("grandchild %d survived cancel", pid)
	}
}
