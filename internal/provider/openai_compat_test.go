package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToOpenAIMessages_AssistantToolCalls(t *testing.T) {
	msgs := []Message{
		{
			Role: RoleAssistant,
			Content: []ContentBlock{
				NewTextContent("reading it"),
				NewToolUseContent("call_01", "read_file", json.RawMessage(`{"path":"main.go"}`)),
			},
		},
	}

	result := toOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("got %d messages, want 1", len(result))
	}

	raw, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal assistant message: %v", err)
	}

	var parsed struct {
		Role      string `json:"role"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal assistant message: %v", err)
	}

	if parsed.Role != "assistant" {
		t.Errorf("role = %q, want assistant", parsed.Role)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("got %d tool_calls, want 1: %s", len(parsed.ToolCalls), raw)
	}
	if parsed.ToolCalls[0].ID != "call_01" {
		t.Errorf("tool call id = %q, want call_01", parsed.ToolCalls[0].ID)
	}
	if parsed.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("tool call name = %q, want read_file", parsed.ToolCalls[0].Function.Name)
	}
	if !strings.Contains(parsed.ToolCalls[0].Function.Arguments, "main.go") {
		t.Errorf("tool call arguments = %q, want path argument", parsed.ToolCalls[0].Function.Arguments)
	}
}

func TestToOpenAIMessages_ToolResult(t *testing.T) {
	msgs := []Message{
		{
			Role:    RoleTool,
			Content: []ContentBlock{NewToolResultContent("call_01", "package main")},
		},
	}

	result := toOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("got %d messages, want 1", len(result))
	}

	raw, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal tool message: %v", err)
	}
	for _, want := range []string{`"tool"`, `"call_01"`, "package main"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("tool message missing %s: %s", want, raw)
		}
	}
}

func TestCombinedText(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []ContentBlock
		expected string
	}{
		{"single text block", []ContentBlock{NewTextContent("hello")}, "hello"},
		{"single tool use block", []ContentBlock{NewToolUseContent("id", "tool", nil)}, ""},
		{"single tool result block", []ContentBlock{NewToolResultContent("id", "output")}, ""},
		{
			"mixed blocks",
			[]ContentBlock{
				NewTextContent("a"),
				NewToolUseContent("id", "tool", nil),
				NewTextContent("b"),
			},
			"ab",
		},
		{"empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := combinedText(tt.blocks); got != tt.expected {
				t.Errorf("combinedText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFromOpenAIFinishReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		expected StopReason
	}{
		{"stop", "stop", StopReasonStop},
		{"length", "length", StopReasonMaxTokens},
		{"tool calls", "tool_calls", StopReasonToolUse},
		{"passthrough", "content_filter", StopReason("content_filter")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromOpenAIFinishReason(tt.reason); got != tt.expected {
				t.Errorf("fromOpenAIFinishReason(%q) = %q, want %q", tt.reason, got, tt.expected)
			}
		})
	}
}

func TestNewOpenAIClient_SetsModel(t *testing.T) {
	client := NewOpenAIClient(OpenAIConfig{APIKey: "test"})
	if got := client.GetCurrentModel(); got != string(OpenAIModelGPT5_4) {
		t.Errorf("GetCurrentModel() = %q, want %q", got, OpenAIModelGPT5_4)
	}

	client = NewOpenAIClient(OpenAIConfig{APIKey: "test", Model: OpenAIModelGPT5_4Mini})
	if got := client.GetCurrentModel(); got != string(OpenAIModelGPT5_4Mini) {
		t.Errorf("GetCurrentModel() = %q, want %q", got, OpenAIModelGPT5_4Mini)
	}
}
