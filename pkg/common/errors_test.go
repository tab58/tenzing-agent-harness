package common

import (
	"errors"
	"fmt"
	"testing"
)

func TestAPIError_Transient(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		want       bool
	}{
		{"400 bad request", 400, nil, false},
		{"401 unauthorized", 401, nil, false},
		{"404 not found", 404, nil, false},
		{"408 timeout", 408, nil, true},
		{"429 rate limited", 429, nil, true},
		{"500 server error", 500, nil, true},
		{"503 unavailable", 503, nil, true},
		{"599 upper bound", 599, nil, true},
		{"network failure", 0, errors.New("connection refused"), true},
		{"zero status without cause", 0, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &APIError{StatusCode: tt.statusCode, Provider: "anthropic", Message: "boom", Err: tt.err}
			if got := e.Transient(); got != tt.want {
				t.Errorf("Transient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIError_ErrorString(t *testing.T) {
	withStatus := &APIError{StatusCode: 503, Provider: "openai", Message: "overloaded"}
	if got, want := withStatus.Error(), "openai API error (status 503): overloaded"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	noStatus := &APIError{Provider: "ollama", Message: "connection refused"}
	if got, want := noStatus.Error(), "ollama API error: connection refused"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAPIError_WrappingRoundTrip(t *testing.T) {
	cause := errors.New("root cause")
	apiErr := &APIError{StatusCode: 429, Provider: "cerebras", Message: "slow down", Err: cause}
	wrapped := fmt.Errorf("cerebras send message: %w", apiErr)

	var got *APIError
	if !errors.As(wrapped, &got) {
		t.Fatalf("errors.As failed to find *APIError in %v", wrapped)
	}
	if got.StatusCode != 429 || got.Provider != "cerebras" {
		t.Errorf("recovered APIError = %+v, want status 429 provider cerebras", got)
	}
	if !errors.Is(wrapped, cause) {
		t.Errorf("errors.Is failed to find the wrapped cause through APIError")
	}
}
