// Package app holds app-level wiring shared by cmd/app: pieces that sit
// above the harness but below the main binary.
package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// LogBroadcaster is an io.Writer that fans each written message out to
// SSE subscribers. Give it to a slog handler via io.MultiWriter so log
// output tees to both the log file and /debug SSE clients. Writes never
// block: slow subscribers drop messages.
type LogBroadcaster struct {
	mu   sync.RWMutex
	subs map[chan string]struct{}
}

func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{subs: make(map[chan string]struct{})}
}

// Write implements io.Writer. Always returns len(p), nil so an
// io.MultiWriter never aborts the file write.
func (b *LogBroadcaster) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		b.mu.RLock()
		for ch := range b.subs {
			select {
			case ch <- msg:
			default: // slow client: drop
			}
		}
		b.mu.RUnlock()
	}
	return len(p), nil
}

func (b *LogBroadcaster) subscribe() chan string {
	ch := make(chan string, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *LogBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

// SSEHandler streams broadcast log lines as SSE "log" events.
func (b *LogBroadcaster) SSEHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		flusher.Flush()

		ch := b.subscribe()
		defer b.unsubscribe(ch)

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ch:
				data := strings.ReplaceAll(msg, "\n", "\ndata: ")
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	})
}
