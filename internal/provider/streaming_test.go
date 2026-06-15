package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// collectEvents drains a stream of events, returning the text deltas and the
// final response. Fails the test on error events or a missing stop event.
func collectEvents(t *testing.T, events <-chan StreamEvent) ([]string, *CompletionResponse) {
	t.Helper()
	var deltas []string
	var response *CompletionResponse
	for ev := range events {
		switch ev.Type {
		case StreamEventDelta:
			deltas = append(deltas, ev.Text)
		case StreamEventStop:
			response = ev.Response
		case StreamEventError:
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
	}
	if response == nil {
		t.Fatal("stream ended without a stop event")
	}
	return deltas, response
}

func TestOpenAICompat_StreamingAccumulatesToolCalls(t *testing.T) {
	chunks := []string{
		`{"id":"chatcmpl-1","model":"test-model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			if _, err := w.Write([]byte("data: " + chunk + "\n\n")); err != nil {
				t.Errorf("write chunk: %v", err)
				return
			}
		}
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			t.Errorf("write done: %v", err)
		}
	}))
	defer srv.Close()

	client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL))
	compat := &openAICompat{name: "test", client: &client}

	events := make(chan StreamEvent, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- compat.SendStreamingMessage(context.Background(), CompletionRequest{
			Model:    "test-model",
			Messages: []Message{NewUserMessage("hi")},
		}, events)
	}()

	deltas, response := collectEvents(t, events)
	if err := <-errCh; err != nil {
		t.Fatalf("SendStreamingMessage: %v", err)
	}

	if got := len(deltas); got != 2 {
		t.Errorf("got %d deltas, want 2: %v", got, deltas)
	}
	if response.Text() != "hello" {
		t.Errorf("accumulated text = %q, want hello", response.Text())
	}

	calls := response.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1; content: %+v", len(calls), response.Content)
	}
	if calls[0].ToolUseID != "call_1" || calls[0].ToolName != "read_file" {
		t.Errorf("tool call = %s/%s, want call_1/read_file", calls[0].ToolUseID, calls[0].ToolName)
	}
	if string(calls[0].ToolInput) != `{"path":"main.go"}` {
		t.Errorf("tool input = %s, want fully assembled JSON", calls[0].ToolInput)
	}

	if response.StopReason != StopReasonToolUse {
		t.Errorf("stop reason = %q, want %q", response.StopReason, StopReasonToolUse)
	}
	if response.Usage.InputTokens != 10 || response.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want 10/5", response.Usage)
	}
}

func TestOpenAICompat_StreamingClosesChannelOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL))
	compat := &openAICompat{name: "test", client: &client}

	events := make(chan StreamEvent, 32)
	err := compat.SendStreamingMessage(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	}, events)
	if err == nil {
		t.Fatal("want error, got nil")
	}

	// Channel must be closed so a ranging consumer terminates.
	sawError := false
	for ev := range events {
		if ev.Type == StreamEventError {
			sawError = true
		}
	}
	if !sawError {
		t.Error("no error event emitted before close")
	}
}

func TestOllama_StreamingToolCalls(t *testing.T) {
	chunks := []string{
		`{"model":"test-model","message":{"role":"assistant","content":"think"},"done":false}`,
		`{"model":"test-model","message":{"role":"assistant","content":"ing"},"done":false}`,
		`{"model":"test-model","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"main.go"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":7,"eval_count":3}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk + "\n")); err != nil {
				t.Errorf("write chunk: %v", err)
				return
			}
		}
	}))
	defer srv.Close()

	ollama := NewOllamaClient(OllamaConfig{BaseURL: srv.URL})

	events := make(chan StreamEvent, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- ollama.SendStreamingMessage(context.Background(), CompletionRequest{
			Model:    "test-model",
			Messages: []Message{NewUserMessage("hi")},
		}, events)
	}()

	deltas, response := collectEvents(t, events)
	if err := <-errCh; err != nil {
		t.Fatalf("SendStreamingMessage: %v", err)
	}

	if got := len(deltas); got != 2 {
		t.Errorf("got %d deltas, want 2: %v", got, deltas)
	}

	calls := response.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1; content: %+v", len(calls), response.Content)
	}
	if calls[0].ToolName != "read_file" {
		t.Errorf("tool name = %q, want read_file", calls[0].ToolName)
	}
	if response.StopReason != StopReasonToolUse {
		t.Errorf("stop reason = %q, want %q", response.StopReason, StopReasonToolUse)
	}
	if response.Usage.InputTokens != 7 || response.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want 7/3", response.Usage)
	}
}
