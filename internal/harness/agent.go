package harness

import "tenzing-agent/internal/tools/tooldef"

type Agent interface {
	DoReasoning(input []string) (ReasoningResult, error)
}

type ReasoningResult struct {
	ToolCall    *tooldef.ToolCall
	FinalAnswer string
}
