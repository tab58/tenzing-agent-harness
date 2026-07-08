// Package nexus turns the app into an information nexus: YAML-configured
// input channels (file-tail, command, webhook) buffer external log/message
// streams for the agent, and error lines can wake the agent automatically.
package nexus

import (
	"slices"
	"sync"
	"time"
)

// Entry is one buffered message on a channel.
type Entry struct {
	Seq     uint64    // per-channel monotonic
	Time    time.Time
	Text    string
	IsError bool // channel error_pattern matched
}

// Ring is a fixed-size ring buffer of entries. Safe for concurrent use.
type Ring struct {
	mu       sync.RWMutex
	entries  []Entry
	size     int
	start    int // index of oldest entry
	count    int
	nextSeq  uint64
	errCount int
}

func NewRing(size int) *Ring {
	return &Ring{entries: make([]Entry, size), size: size}
}

// Append stores a new entry, evicting the oldest when full, and returns it.
func (r *Ring) Append(text string, isError bool) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	e := Entry{Seq: r.nextSeq, Time: time.Now(), Text: text, IsError: isError}
	r.nextSeq++

	if r.count == r.size {
		if r.entries[r.start].IsError {
			r.errCount--
		}
		r.entries[r.start] = e
		r.start = (r.start + 1) % r.size
	} else {
		r.entries[(r.start+r.count)%r.size] = e
		r.count++
	}
	if isError {
		r.errCount++
	}
	return e
}

// Last returns up to n most recent entries in oldest→newest order,
// optionally filtered to error entries.
func (r *Ring) Last(n int, errorsOnly bool) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n < 0 {
		n = 0
	}
	out := make([]Entry, 0, min(n, r.count))
	for i := r.count - 1; i >= 0 && len(out) < n; i-- {
		e := r.entries[(r.start+i)%r.size]
		if errorsOnly && !e.IsError {
			continue
		}
		out = append(out, e)
	}
	slices.Reverse(out)
	return out
}

// Snapshot returns all buffered entries in oldest→newest order.
func (r *Ring) Snapshot() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Entry, r.count)
	for i := 0; i < r.count; i++ {
		out[i] = r.entries[(r.start+i)%r.size]
	}
	return out
}

// Counts returns total buffered entries and how many are errors.
func (r *Ring) Counts() (total, errors int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count, r.errCount
}
