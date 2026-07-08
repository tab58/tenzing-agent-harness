package nexus

import (
	"sort"
	"sync"
	"time"
)

// Trigger converts channel error notifications into agent wake attempts.
// Per-channel cooldown stops error storms from re-notifying; a global
// queue-of-one holds pending channel names while the agent is busy and
// flushes them when TurnEnded is called.
type Trigger struct {
	mu         sync.Mutex
	cooldown   time.Duration
	wake       func(channels []string) bool
	lastNotify map[string]time.Time
	pending    map[string]struct{}
}

// NewTrigger builds a trigger. wake starts an agent turn for the given
// channels and returns false when the agent is busy (channels stay pending).
func NewTrigger(cooldown time.Duration, wake func(channels []string) bool) *Trigger {
	return &Trigger{
		cooldown:   cooldown,
		wake:       wake,
		lastNotify: make(map[string]time.Time),
		pending:    make(map[string]struct{}),
	}
}

// Notify records an error on the named channel and attempts a wake unless
// the channel is inside its cooldown window.
func (t *Trigger) Notify(channel string) {
	t.mu.Lock()
	now := time.Now()
	if last, ok := t.lastNotify[channel]; ok && now.Sub(last) < t.cooldown {
		t.mu.Unlock()
		return
	}
	t.lastNotify[channel] = now
	t.pending[channel] = struct{}{}
	t.mu.Unlock()

	t.tryFire()
}

// TurnEnded flushes pending channels, if any. Wire it to the agent turn's
// completion.
func (t *Trigger) TurnEnded() {
	t.tryFire()
}

func (t *Trigger) tryFire() {
	t.mu.Lock()
	if len(t.pending) == 0 {
		t.mu.Unlock()
		return
	}
	channels := make([]string, 0, len(t.pending))
	for c := range t.pending {
		channels = append(channels, c)
	}
	sort.Strings(channels)
	t.mu.Unlock()

	// wake outside the lock: it broadcasts and starts a goroutine
	if t.wake(channels) {
		t.mu.Lock()
		for _, c := range channels {
			delete(t.pending, c)
		}
		t.mu.Unlock()
	}
}
