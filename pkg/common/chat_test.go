package common

import "testing"

func TestCompletionResponseText(t *testing.T) {
	tests := []struct {
		name     string
		content  []ContentBlock
		expected string
	}{
		{"text block", []ContentBlock{NewTextContent("hello")}, "hello"},
		{"no content", nil, ""},
		{"tool use only", []ContentBlock{NewToolUseContent("id", "tool", nil)}, ""},
		{
			"first text block wins",
			[]ContentBlock{
				NewToolUseContent("id", "tool", nil),
				NewTextContent("a"),
				NewTextContent("b"),
			},
			"a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := CompletionResponse{Content: tt.content}
			if got := res.Text(); got != tt.expected {
				t.Errorf("Text() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestThinkingContent(t *testing.T) {
	res := CompletionResponse{Content: []ContentBlock{
		NewThinkingContent("step 1"),
		NewThinkingContent("step 2"),
		NewTextContent("answer"),
	}}
	if got := res.Text(); got != "answer" {
		t.Errorf("Text() = %q, want %q (thinking blocks must not leak into text)", got, "answer")
	}
	if got := res.Thinking(); got != "step 1step 2" {
		t.Errorf("Thinking() = %q, want %q", got, "step 1step 2")
	}
	if got := CombinedText(res.Content); got != "answer" {
		t.Errorf("CombinedText() = %q, want %q (thinking excluded from resend text)", got, "answer")
	}
}

func TestMessageConstructors(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		wantRole Role
		wantText string
	}{
		{"user", NewUserMessage("hi"), RoleUser, "hi"},
		{"assistant", NewAssistantMessage("hello"), RoleAssistant, "hello"},
		{"system", NewSystemMessage("rules"), RoleSystem, "rules"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.Role != tt.wantRole {
				t.Errorf("role = %q, want %q", tt.msg.Role, tt.wantRole)
			}
			if len(tt.msg.Content) != 1 || tt.msg.Content[0].Text != tt.wantText {
				t.Errorf("content = %+v, want single text block %q", tt.msg.Content, tt.wantText)
			}
		})
	}
}
