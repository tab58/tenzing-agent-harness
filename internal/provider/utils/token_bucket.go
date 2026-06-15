package utils

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// TokenBucketConfig configures a token bucket rate limiter.
type TokenBucketConfig struct {
	// Rate is the refill rate in tokens per second.
	Rate float64
	// BurstSize is the maximum number of tokens the bucket can hold (burst capacity).
	BurstSize int64
	// MaxConcurrency limits simultaneous in-flight operations. Zero means unlimited.
	MaxConcurrency int64
}

// TokenBucket implements a token-bucket rate limiter suitable for controlling
// LLM API throughput by input token cost. It combines a token budget that
// refills at a steady rate with an optional concurrency semaphore.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	burstSize  float64
	rate       float64
	lastRefill time.Time
	sem        *semaphore.Weighted
	now        func() time.Time // for testing
}

// NewTokenBucket creates a TokenBucket from the given config. The bucket
// starts full (at burst capacity).
func NewTokenBucket(cfg TokenBucketConfig) *TokenBucket {
	tb := &TokenBucket{
		tokens:     float64(cfg.BurstSize),
		burstSize:  float64(cfg.BurstSize),
		rate:       cfg.Rate,
		lastRefill: time.Now(),
		now:        time.Now,
	}
	if cfg.MaxConcurrency > 0 {
		tb.sem = semaphore.NewWeighted(cfg.MaxConcurrency)
	}
	return tb
}

// Acquire blocks until cost tokens are available in the bucket or ctx is
// cancelled. If a concurrency semaphore is configured, it is acquired first
// (one slot per call) and released only when Acquire returns an error —
// on success the caller must call Release when the work is done.
func (tb *TokenBucket) Acquire(ctx context.Context, cost int64) error {
	if tb.sem != nil {
		if err := tb.sem.Acquire(ctx, 1); err != nil {
			return err
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			if tb.sem != nil {
				tb.sem.Release(1)
			}
			return err
		}

		tb.mu.Lock()
		tb.refill()

		fcost := float64(cost)
		if tb.tokens >= fcost {
			tb.tokens -= fcost
			tb.mu.Unlock()
			return nil
		}

		// Calculate how long until enough tokens are available.
		deficit := fcost - tb.tokens
		wait := time.Duration(math.Ceil(deficit/tb.rate*1000)) * time.Millisecond
		wait = max(wait, time.Millisecond)
		// Cap the poll interval to avoid sleeping too long.
		wait = min(wait, 100*time.Millisecond)
		tb.mu.Unlock()

		select {
		case <-ctx.Done():
			if tb.sem != nil {
				tb.sem.Release(1)
			}
			return ctx.Err()
		case <-time.After(wait):
			// retry
		}
	}
}

// Release returns one concurrency slot to the semaphore. Must be called
// after a successful Acquire once the work is complete.
func (tb *TokenBucket) Release() {
	if tb.sem != nil {
		tb.sem.Release(1)
	}
}

// refill adds tokens based on elapsed time since the last refill.
// Must be called under tb.mu.
func (tb *TokenBucket) refill() {
	now := tb.now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.burstSize {
		tb.tokens = tb.burstSize
	}
	tb.lastRefill = now
}
