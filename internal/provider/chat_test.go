package provider

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
