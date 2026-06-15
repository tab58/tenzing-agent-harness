package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const compatCompletionJSON = `{
	"id": "chatcmpl-1", "object": "chat.completion", "model": "test-model",
	"choices": [{"index": 0, "message": {"role": "assistant", "content": "hello"}, "finish_reason": "stop"}],
	"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}`

// newRetryTestCompat returns a retrying client whose first n requests are
// rejected with 429, plus a counter of requests received. SDK-internal
// retries are disabled so the provider's retry loop is what's under test.
func newRetryTestCompat(t *testing.T, reject429 int32, respond http.HandlerFunc) (*openAICompat, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= reject429 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error": {"message": "rate limited", "type": "rate_limit_error"}}`)
			return
		}
		respond(w, r)
	}))
	t.Cleanup(srv.Close)

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(srv.URL),
		option.WithMaxRetries(0),
	)
	return &openAICompat{
		name:           "test",
		client:         &client,
		retryRateLimit: true,
		baseBackoff:    time.Millisecond,
		maxBackoff:     5 * time.Millisecond,
	}, &requests
}

func respondCompletion(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(compatCompletionJSON)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"api error 429", &openai.Error{StatusCode: 429}, true},
		{"wrapped api error 429", fmt.Errorf("send: %w", &openai.Error{StatusCode: 429}), true},
		{"api error 500", &openai.Error{StatusCode: 500}, false},
		{"string fallback", errors.New("unexpected status 429 Too Many Requests"), true},
		{"unrelated error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRateLimitError(tt.err); got != tt.expected {
				t.Errorf("isRateLimitError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOpenAICompat_SyncRetriesOnRateLimit(t *testing.T) {
	compat, requests := newRetryTestCompat(t, 2, respondCompletion(t))

	res, err := compat.SendSyncMessage(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("SendSyncMessage: %v", err)
	}

	if got := requests.Load(); got != 3 {
		t.Errorf("server received %d requests, want 3 (two 429s then success)", got)
	}
	if res.Text() != "hello" {
		t.Errorf("text = %q, want hello", res.Text())
	}
}

func TestOpenAICompat_SyncRetryExhausted(t *testing.T) {
	compat, requests := newRetryTestCompat(t, int32(compatMaxRetries)+1, respondCompletion(t))

	_, err := compat.SendSyncMessage(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err == nil {
		t.Fatal("want error after exhausting retries, got nil")
	}
	if got := requests.Load(); got != int32(compatMaxRetries) {
		t.Errorf("server received %d requests, want %d", got, compatMaxRetries)
	}
}

func TestOpenAICompat_NoRetryWhenDisabled(t *testing.T) {
	compat, requests := newRetryTestCompat(t, 1, respondCompletion(t))
	compat.retryRateLimit = false

	_, err := compat.SendSyncMessage(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("server received %d requests, want 1 (no retry)", got)
	}
}

func TestOpenAICompat_StreamingRetriesBeforeFirstEvent(t *testing.T) {
	compat, requests := newRetryTestCompat(t, 1, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"id":"chatcmpl-1","model":"test-model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, chunk := range chunks {
			if _, err := w.Write([]byte("data: " + chunk + "\n\n")); err != nil {
				t.Errorf("write chunk: %v", err)
				return
			}
		}
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			t.Errorf("write done: %v", err)
		}
	})

	events := make(chan StreamEvent, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- compat.SendStreamingMessage(context.Background(), CompletionRequest{
			Model:    "test-model",
			Messages: []Message{NewUserMessage("hi")},
		}, events)
	}()

	starts := 0
	var deltas []string
	for ev := range events {
		switch ev.Type {
		case StreamEventStart:
			starts++
		case StreamEventDelta:
			deltas = append(deltas, ev.Text)
		case StreamEventError:
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("SendStreamingMessage: %v", err)
	}

	if got := requests.Load(); got != 2 {
		t.Errorf("server received %d requests, want 2 (one 429 then stream)", got)
	}
	if starts != 1 {
		t.Errorf("got %d start events, want exactly 1 (no duplicates from retry)", starts)
	}
	if len(deltas) != 1 || deltas[0] != "hello" {
		t.Errorf("deltas = %v, want [hello]", deltas)
	}
}
