// Package core holds the invariant agent loop, its domain types, port
// interfaces, and events. It imports nothing from internal/ — adapters and
// extensions import core, never the reverse.
package core

// ToolCall is one tool_use request from the model.
type ToolCall struct {
	ID    string
	Name  string
	Input string
}

// ToolResult is the outcome of executing one ToolCall. Failures are data
// (IsError), never Go errors — the model reacts to them.
type ToolResult struct {
	ToolUseID string
	Output    string
	IsError   bool
	Metadata  map[string]string
}
