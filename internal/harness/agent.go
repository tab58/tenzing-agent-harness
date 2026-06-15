package harness

type Agent interface {
	DoReasoning(input []string) (ReasoningResult, error)
	ExecuteTool() (string, error)
}

type ToolResult struct {
	ToolUseID string // matches ToolUse.ID
	Output    string
	IsError   bool
}

type ToolCall struct {
	Name  string
	Input string
}

type ReasoningResult struct {
	ToolCall    *ToolCall
	FinalAnswer string
}
