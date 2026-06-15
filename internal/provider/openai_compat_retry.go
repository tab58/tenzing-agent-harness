package provider

// Rate-limit detection and retry/backoff helpers for the OpenAI-compatible
// client core in openai_compat.go.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429
	}
	// Fallback for providers that wrap errors outside the SDK type.
	msg := err.Error()
	return strings.Contains(msg, "429") && strings.Contains(msg, "Too Many Requests")
}

func rateLimitBackoff(ctx context.Context, name string, attempt int, base, maxBackoff time.Duration) error {
	backoff := float64(base) * math.Pow(2, float64(attempt))
	backoff = min(backoff, float64(maxBackoff))
	jitter := backoff * compatBackoffJitter * (rand.Float64()*2 - 1)
	delay := time.Duration(backoff + jitter)

	slog.WarnContext(ctx, "API rate limited, backing off",
		"provider", name,
		"attempt", attempt+1,
		"delay", delay.Round(time.Millisecond),
	)

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryOnRateLimit[T any](ctx context.Context, name string, maxAttempts int, backoff func(context.Context, int) error, fn func() (T, error)) (T, error) {
	var zero T
	for attempt := range maxAttempts {
		result, err := fn()
		if err == nil || !isRateLimitError(err) {
			return result, err
		}
		if attempt == maxAttempts-1 {
			return zero, fmt.Errorf("%s rate limited after %d attempts: %w", name, maxAttempts, err)
		}
		if err := backoff(ctx, attempt); err != nil {
			return zero, err
		}
	}
	return zero, nil
}
