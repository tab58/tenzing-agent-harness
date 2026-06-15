package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tenzing-agent/internal/provider/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func newCompatTestClient(t *testing.T, handler http.HandlerFunc) *openAICompat {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(srv.URL),
		option.WithMaxRetries(0),
	)
	return &openAICompat{name: "test", client: &client, model: "test-model"}
}

func TestOpenAICompat_SendSyncMessage(t *testing.T) {
	compat := newCompatTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "test-model",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "reading",
					"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"main.go\"}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19}
		}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	})

	res, err := compat.SendSyncMessage(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("read main.go")},
	})
	if err != nil {
		t.Fatalf("SendSyncMessage: %v", err)
	}

	if res.Text() != "reading" {
		t.Errorf("text = %q, want reading", res.Text())
	}
	calls := res.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].ToolUseID != "call_1" || calls[0].ToolName != "read_file" {
		t.Errorf("tool call = %s/%s, want call_1/read_file", calls[0].ToolUseID, calls[0].ToolName)
	}
	if res.StopReason != StopReasonToolUse {
		t.Errorf("stop reason = %q, want %q", res.StopReason, StopReasonToolUse)
	}
	if res.Usage.InputTokens != 12 || res.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v, want 12/7", res.Usage)
	}
}

func TestOpenAICompat_SendMessageWithToolsWireShape(t *testing.T) {
	var requestBody []byte
	compat := newCompatTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var err error
		requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(compatCompletionJSON)); err != nil {
			t.Errorf("write response: %v", err)
		}
	})

	_, err := compat.SendMessageWithTools(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	}, []ToolDefinition{{
		Name:        "read_file",
		Description: "Reads a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}})
	if err != nil {
		t.Fatalf("SendMessageWithTools: %v", err)
	}

	var wire struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name       string `json:"name"`
				Parameters struct {
					Type       string                     `json:"type"`
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(requestBody, &wire); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(wire.Tools) != 1 {
		t.Fatalf("got %d tools on the wire, want 1: %s", len(wire.Tools), requestBody)
	}
	fn := wire.Tools[0].Function
	if fn.Name != "read_file" {
		t.Errorf("wire tool name = %q, want read_file", fn.Name)
	}
	if _, ok := fn.Parameters.Properties["path"]; !ok {
		t.Errorf("wire parameters missing path property: %s", requestBody)
	}
	if len(fn.Parameters.Required) != 1 || fn.Parameters.Required[0] != "path" {
		t.Errorf("wire parameters required = %v, want [path]", fn.Parameters.Required)
	}
}

func TestOpenAICompat_BadToolSchemaRejected(t *testing.T) {
	compat := newCompatTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("request should not reach the server")
	})

	_, err := compat.SendMessageWithTools(context.Background(), CompletionRequest{
		Model:    "test-model",
		Messages: []Message{NewUserMessage("hi")},
	}, []ToolDefinition{{
		Name:        "bad",
		InputSchema: json.RawMessage(`{not json`),
	}})
	if err == nil {
		t.Fatal("want error for invalid schema, got nil")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should name the tool: %v", err)
	}
}

func TestOpenAICompat_ListModels(t *testing.T) {
	compat := newCompatTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"object": "list", "data": [
			{"id": "model-a", "object": "model", "created": 0, "owned_by": "test"},
			{"id": "model-b", "object": "model", "created": 0, "owned_by": "test"}
		]}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	})

	models, err := compat.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "model-a" || models[1].ID != "model-b" {
		t.Errorf("models = %+v, want model-a and model-b", models)
	}
}

func TestOpenAICompat_RateLimitAcquireRelease(t *testing.T) {
	compat := newCompatTestClient(t, respondCompletion(t))
	compat.rateLimiter = utils.NewTokenBucket(utils.TokenBucketConfig{
		Rate:           100_000,
		BurstSize:      100_000,
		MaxConcurrency: 1,
	})
	compat.tokenCostLimit = true

	// Two sequential sends must both succeed: if Release is broken, the
	// second send deadlocks on the MaxConcurrency=1 semaphore.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range 2 {
		if _, err := compat.SendSyncMessage(ctx, CompletionRequest{
			Model:    "test-model",
			Messages: []Message{NewUserMessage("hi")},
		}); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
}

func TestOpenAICompat_CountTokensEstimate(t *testing.T) {
	compat := &openAICompat{name: "test"}

	count, err := compat.CountTokens(context.Background(), CompletionRequest{
		System: strings.Repeat("s", 40),
		Messages: []Message{
			{Role: RoleUser, Content: []ContentBlock{NewTextContent(strings.Repeat("m", 60))}},
		},
	})
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	// (40 + 60) chars / 4 chars per token.
	if count.InputTokens != 25 {
		t.Errorf("input tokens = %d, want 25", count.InputTokens)
	}
}
