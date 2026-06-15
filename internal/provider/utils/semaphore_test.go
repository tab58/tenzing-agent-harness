package utils

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphore_LimitsConcurrency(t *testing.T) {
	sem := NewSemaphore(2)
	ctx := context.Background()

	var active, peak atomic.Int64
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if err := sem.Acquire(ctx); err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			defer sem.Release()

			cur := active.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
		})
	}
	wg.Wait()

	if got := peak.Load(); got > 2 {
		t.Errorf("peak concurrency = %d, want <= 2", got)
	}
}

func TestSemaphore_ContextCancellation(t *testing.T) {
	sem := NewSemaphore(1)
	ctx := context.Background()

	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer sem.Release()

	ctx2, cancel := context.WithCancel(ctx)
	cancel()

	if err := sem.Acquire(ctx2); err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}
