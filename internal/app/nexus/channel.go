package nexus

import (
	"fmt"
	"regexp"
	"sync"
)

// Channel is the runtime for one configured input channel: a ring buffer,
// a compiled error pattern, and a status. Sources call Ingest for each
// produced line/message.
type Channel struct {
	cfg     ChannelConfig
	re      *regexp.Regexp
	ring    *Ring
	onError func(Entry)

	mu     sync.RWMutex
	status string
}

func newChannel(cfg ChannelConfig, onError func(Entry)) (*Channel, error) {
	re, err := regexp.Compile(cfg.ErrorPattern)
	if err != nil {
		return nil, fmt.Errorf("channel %q: error_pattern: %w", cfg.Name, err)
	}
	return &Channel{
		cfg:     cfg,
		re:      re,
		ring:    NewRing(cfg.BufferSize),
		onError: onError,
		status:  "stopped",
	}, nil
}

// Ingest buffers one message, flagging it when the error pattern matches
// and firing the onError callback.
func (c *Channel) Ingest(text string) Entry {
	isError := c.re.MatchString(text)
	e := c.ring.Append(text, isError)
	if isError && c.onError != nil {
		c.onError(e)
	}
	return e
}

func (c *Channel) Status() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Channel) setStatus(s string) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
}
