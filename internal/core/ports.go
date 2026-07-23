package core

import (
	"context"

	"github.com/tab58/llm-providers/common"
)

// ContextPort owns the conversation history: user/assistant messages, tool
// result pairing, and compression. Adapters implement it; the core loop
// depends only on this interface.
type ContextPort interface {
	Messages(ctx context.Context) ([]common.Message, error)
	AppendUser(ctx context.Context, text string) error
	AppendAssistant(ctx context.Context, msg common.Message) error
	AppendToolResults(ctx context.Context, results []ToolResult) error
}

// ModelPort is a stateless model call: given the full message history, system
// reminders, and the tool definitions for this turn, it returns tool calls to
// execute or a final answer. Adapters implement it; the core loop depends
// only on this interface.
type ModelPort interface {
	DoReasoning(ctx context.Context, messages []common.Message, systemReminders []string, tools []common.ToolDefinition) (ReasoningResult, error)
}

// ToolPort owns tool definitions and execution. BeginTurn is called once by
// the loop at turn start so composite ports can snapshot dynamic sources.
type ToolPort interface {
	BeginTurn(ctx context.Context)
	Definitions() []common.ToolDefinition
	Origin(name string) string
	Execute(ctx context.Context, call ToolCall) ToolResult
}
