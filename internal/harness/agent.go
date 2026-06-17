package harness

import (
	"context"

	"tenzing-agent/internal/tools/tooldef"
)

type Agent interface {
	DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (ReasoningResult, error)
}

type ReasoningResult struct {
	ToolCall    *tooldef.ToolCall
	FinalAnswer string
}
