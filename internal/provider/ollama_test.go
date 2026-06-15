package provider

import (
	"encoding/json"
	"testing"
)

func TestToOllamaMessages_AssistantToolCalls(t *testing.T) {
	req := CompletionRequest{
		Messages: []Message{
			{
				Role: RoleAssistant,
				Content: []ContentBlock{
					NewTextContent("reading it"),
					NewToolUseContent("call_01", "read_file", json.RawMessage(`{"path":"main.go"}`)),
				},
			},
		},
	}

	msgs := toOllamaMessages(req, nil)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}

	msg := msgs[0]
	if msg.Role != "assistant" {
		t.Errorf("role = %q, want assistant", msg.Role)
	}
	if msg.Content != "reading it" {
		t.Errorf("content = %q, want %q", msg.Content, "reading it")
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("tool call name = %q, want read_file", msg.ToolCalls[0].Function.Name)
	}
	if msg.ToolCalls[0].Function.Arguments["path"] != "main.go" {
		t.Errorf("tool call arguments = %v, want path=main.go", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestToOllamaMessages_RolesAndToolResults(t *testing.T) {
	req := CompletionRequest{
		System: "be helpful",
		Messages: []Message{
			NewUserMessage("hi"),
			NewAssistantMessage("hello"),
			{
				Role:    RoleTool,
				Content: []ContentBlock{NewToolResultContent("call_01", "package main")},
			},
		},
	}

	msgs := toOllamaMessages(req, nil)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}

	expected := []struct{ role, content string }{
		{"system", "be helpful"},
		{"user", "hi"},
		{"assistant", "hello"},
		{"tool", "package main"},
	}
	for i, want := range expected {
		if msgs[i].Role != want.role || msgs[i].Content != want.content {
			t.Errorf("msg[%d] = {%q, %q}, want {%q, %q}",
				i, msgs[i].Role, msgs[i].Content, want.role, want.content)
		}
	}
}

func TestFromOllamaResponse_ToolCalls(t *testing.T) {
	res := ollamaChatResponse{
		Model: "qwen3.5:9b",
		Message: ollamaChatMessage{
			Role: "assistant",
			ToolCalls: []ollamaToolCall{
				{Function: ollamaToolCallFunction{
					Name:      "read_file",
					Arguments: map[string]any{"path": "main.go"},
				}},
			},
		},
		Done:            true,
		DoneReason:      "stop",
		PromptEvalCount: 10,
		EvalCount:       5,
	}

	out := fromOllamaResponse(res, nil)
	if out.StopReason != StopReasonToolUse {
		t.Errorf("stop reason = %q, want %q", out.StopReason, StopReasonToolUse)
	}
	calls := out.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].ToolName != "read_file" {
		t.Errorf("tool name = %q, want read_file", calls[0].ToolName)
	}
	if out.Usage.InputTokens != 10 || out.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want 10/5", out.Usage)
	}
}

func TestStripThinkBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no think block", "hello", "hello"},
		{"think block stripped", "<think>reasoning</think>answer", "answer"},
		{"multiline think block", "<think>line1\nline2</think>\nanswer", "answer"},
		{"empty after strip", "<think>only thoughts</think>", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripThinkBlocks(tt.input); got != tt.expected {
				t.Errorf("StripThinkBlocks(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFromOllamaStopReason(t *testing.T) {
	tests := []struct {
		name     string
		res      ollamaChatResponse
		expected StopReason
	}{
		{"stop", ollamaChatResponse{DoneReason: "stop"}, StopReasonStop},
		{"length", ollamaChatResponse{DoneReason: "length"}, StopReasonMaxTokens},
		{
			"tool calls win over done reason",
			ollamaChatResponse{
				DoneReason: "stop",
				Message:    ollamaChatMessage{ToolCalls: []ollamaToolCall{{}}},
			},
			StopReasonToolUse,
		},
		{"passthrough", ollamaChatResponse{DoneReason: "load"}, StopReason("load")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromOllamaStopReason(tt.res); got != tt.expected {
				t.Errorf("fromOllamaStopReason() = %q, want %q", got, tt.expected)
			}
		})
	}
}
