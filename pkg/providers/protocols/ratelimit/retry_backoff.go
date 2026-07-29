package ratelimit

import "time"

const (
	compatMaxRetries    = 5
	compatBaseBackoff   = 2 * time.Second
	compatMaxBackoff    = 60 * time.Second
	compatBackoffJitter = 0.5
)

type RetryBackoff struct {
	MaxRetries    int
	BackoffJitter float64
	MaxBackoff    time.Duration
	BaseBackoff   time.Duration
}

func NewDefaultBackoff() RetryBackoff {
	return RetryBackoff{
		MaxRetries:    compatMaxRetries,
		BackoffJitter: compatBackoffJitter,
		MaxBackoff:    compatMaxBackoff,
		BaseBackoff:   compatBaseBackoff,
	}
}

// OrDefaults returns r with zero (unset) fields replaced by the default
// values from NewDefaultBackoff.
func (r RetryBackoff) OrDefaults() RetryBackoff {
	d := NewDefaultBackoff()
	if r.MaxRetries <= 0 {
		r.MaxRetries = d.MaxRetries
	}
	if r.BackoffJitter <= 0 {
		r.BackoffJitter = d.BackoffJitter
	}
	if r.MaxBackoff <= 0 {
		r.MaxBackoff = d.MaxBackoff
	}
	if r.BaseBackoff <= 0 {
		r.BaseBackoff = d.BaseBackoff
	}
	return r
}
