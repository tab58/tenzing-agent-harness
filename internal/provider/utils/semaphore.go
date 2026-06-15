package utils

import "context"

// Semaphore limits the number of concurrent operations.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a semaphore that allows up to n concurrent acquisitions.
func NewSemaphore(n int) *Semaphore {
	return &Semaphore{ch: make(chan struct{}, n)}
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a slot. Must be called once for each successful Acquire.
func (s Semaphore) Release() {
	<-s.ch
}
