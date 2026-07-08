package nexus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// lineCollector gathers ingested lines thread-safely.
type lineCollector struct {
	mu    sync.Mutex
	lines []string
}

func (lc *lineCollector) ingest(s string) {
	lc.mu.Lock()
	lc.lines = append(lc.lines, s)
	lc.mu.Unlock()
}

func (lc *lineCollector) snapshot() []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return append([]string(nil), lc.lines...)
}

// waitFor polls until cond returns true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestFileTailAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old-line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runFileTail(ctx, path, 10*time.Millisecond, lc.ingest, func(string) {})
		close(done)
	}()

	// give the tail a moment to open and seek to end
	time.Sleep(50 * time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("new-1\nnew-2\n")
	f.Close()

	if !waitFor(t, 2*time.Second, func() bool { return len(lc.snapshot()) == 2 }) {
		t.Fatalf("lines = %v, want [new-1 new-2]", lc.snapshot())
	}
	got := lc.snapshot()
	if got[0] != "new-1" || got[1] != "new-2" {
		t.Errorf("lines = %v, want [new-1 new-2]", got)
	}
	// old-line must NOT appear (seek to end on start)
	for _, l := range got {
		if l == "old-line" {
			t.Error("tail read pre-existing content; should seek to end")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runFileTail did not return after cancel")
	}
}

func TestFileTailRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("start\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runFileTail(ctx, path, 10*time.Millisecond, lc.ingest, func(string) {})

	time.Sleep(50 * time.Millisecond)

	// simulate rotation: replace the file entirely (new inode)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after-rotate\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		for _, l := range lc.snapshot() {
			if l == "after-rotate" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("rotated content not picked up; lines = %v", lc.snapshot())
	}
}

func TestFileTailMissingFileAppearsLater(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "late.log")

	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runFileTail(ctx, path, 10*time.Millisecond, lc.ingest, func(string) {})

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 2*time.Second, func() bool { return len(lc.snapshot()) >= 1 }) {
		t.Fatalf("late-created file not picked up; lines = %v", lc.snapshot())
	}
}

func TestFileTailCopyTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	// enough content that the tail's offset sits well past the new head
	old := strings.Repeat("old-line\n", 20)
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	lc := &lineCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// long poll so the truncate+rewrite below lands inside ONE poll window:
	// the size-shrink check never observes size < offset
	go runFileTail(ctx, path, 300*time.Millisecond, lc.ingest, func(string) {})

	// give the tail a moment to open and seek to end
	time.Sleep(50 * time.Millisecond)

	// prove the tail is live and its offset is at EOF
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("marker\n")
	f.Close()
	if !waitFor(t, 3*time.Second, func() bool {
		for _, l := range lc.snapshot() {
			if l == "marker" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("marker never ingested; lines = %v", lc.snapshot())
	}

	// logrotate copytruncate: same inode, truncate + rewrite in one shot,
	// new content LONGER than the old offset so the size check passes
	newContent := "new-head\n" + strings.Repeat("filler-line-after-rotate\n", 30)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	// the tail must detect the in-place replacement and re-read from the
	// start: "new-head" is BEFORE the old offset and is only ingested if
	// the reopen happened
	if !waitFor(t, 3*time.Second, func() bool {
		for _, l := range lc.snapshot() {
			if l == "new-head" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("copytruncate not detected, head content missed; lines = %v", lc.snapshot())
	}
}
