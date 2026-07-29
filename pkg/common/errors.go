package common

import "fmt"

// APIError is a provider API failure carrying enough structure for callers to
// classify it with errors.As instead of substring matching. Providers wrap
// SDK/HTTP errors into this type on every request path, including errors
// delivered via StreamEventError.
type APIError struct {
	// StatusCode is the HTTP status of the failed response, or 0 when no
	// HTTP response was received (connection failure, mid-stream drop).
	StatusCode int
	Provider   string
	Message    string
	// Err is the wrapped cause (SDK or transport error), nil when the
	// failure is described entirely by StatusCode/Message.
	Err error
}

func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("%s API error: %s", e.Provider, e.Message)
	}
	return fmt.Sprintf("%s API error (status %d): %s", e.Provider, e.StatusCode, e.Message)
}

func (e *APIError) Unwrap() error { return e.Err }

// Transient reports whether retrying the request could plausibly succeed:
// timeouts (408), rate limits (429), server errors (5xx), or a network
// failure with no HTTP response at all.
func (e *APIError) Transient() bool {
	switch {
	case e.StatusCode == 408, e.StatusCode == 429:
		return true
	case e.StatusCode >= 500 && e.StatusCode <= 599:
		return true
	case e.StatusCode == 0 && e.Err != nil:
		return true
	}
	return false
}
