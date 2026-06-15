package utils

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenBucket_AcquireBasic(t *testing.T) {
	tests := []struct {
		name      string
		burstSize int64
		rate      float64
		cost      int64
		wantErr   bool
	}{
		{
			name:      "cost within burst",
			burstSize: 1000,
			rate:      100,
			cost:      500,
			wantErr:   false,
		},
		{
			name:      "cost equals burst",
			burstSize: 1000,
			rate:      100,
			cost:      1000,
			wantErr:   false,
		},
		{
			name:      "zero cost",
			burstSize: 1000,
			rate:      100,
			cost:      0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := NewTokenBucket(TokenBucketConfig{
				Rate:      tt.rate,
				BurstSize: tt.burstSize,
			})

			ctx := context.Background()
			err := bucket.Acquire(ctx, tt.cost)
			if (err != nil) != tt.wantErr {
				t.Errorf("Acquire() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTokenBucket_BurstCapacity(t *testing.T) {
	bucket := NewTokenBucket(TokenBucketConfig{
		Rate:      100, // 100 tokens/sec
		BurstSize: 1000,
	})

	ctx := context.Background()

	// First acquire drains 600 tokens (1000 -> 400)
	if err := bucket.Acquire(ctx, 600); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire drains 400 tokens (400 -> 0)
	if err := bucket.Acquire(ctx, 400); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	// Third acquire of 200 should block because bucket is empty.
	// Use a short timeout to verify it blocks.
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := bucket.Acquire(ctx2, 200)
	if err == nil {
		t.Error("expected Acquire to block and timeout, but it succeeded")
	}
}

func TestTokenBucket_RefillOverTime(t *testing.T) {
	bucket := NewTokenBucket(TokenBucketConfig{
		Rate:      10000, // 10k tokens/sec = 10 tokens/ms
		BurstSize: 10000,
	})

	ctx := context.Background()

	// Drain the bucket
	if err := bucket.Acquire(ctx, 10000); err != nil {
		t.Fatalf("drain acquire failed: %v", err)
	}

	// Wait for refill (100ms at 10k/sec = ~1000 tokens)
	time.Sleep(120 * time.Millisecond)

	// Should be able to acquire ~1000 tokens now
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := bucket.Acquire(ctx2, 500)
	if err != nil {
		t.Errorf("expected refill to allow acquire, got: %v", err)
	}
}

func TestTokenBucket_ContextCancellation(t *testing.T) {
	bucket := NewTokenBucket(TokenBucketConfig{
		Rate:      1, // very slow refill
		BurstSize: 100,
	})

	ctx := context.Background()

	// Drain the bucket
	if err := bucket.Acquire(ctx, 100); err != nil {
		t.Fatalf("drain acquire failed: %v", err)
	}

	// Cancel context immediately
	ctx2, cancel := context.WithCancel(ctx)
	cancel()

	err := bucket.Acquire(ctx2, 50)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestTokenBucket_MaxConcurrency(t *testing.T) {
	bucket := NewTokenBucket(TokenBucketConfig{
		Rate:           100000,
		BurstSize:      100000,
		MaxConcurrency: 2,
	})

	ctx := context.Background()
	var active atomic.Int64
	var maxActive atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := bucket.Acquire(ctx, 1); err != nil {
				return
			}
			defer bucket.Release()

			cur := active.Add(1)
			// Track peak concurrency
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
		}()
	}
	wg.Wait()

	peak := maxActive.Load()
	if peak > 2 {
		t.Errorf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestTokenBucket_NoConcurrencyLimit(t *testing.T) {
	bucket := NewTokenBucket(TokenBucketConfig{
		Rate:           100000,
		BurstSize:      100000,
		MaxConcurrency: 0, // unlimited
	})

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bucket.Acquire(ctx, 1); err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			bucket.Release()
		}()
	}
	wg.Wait()
}
