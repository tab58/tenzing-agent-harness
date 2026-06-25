package harness

import (
	"context"
	"fmt"
	"strings"

	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

type Agent interface {
	UpdateToolDefinitions(tooldefs []provider.ToolDefinition)
	UpdateSkillMap(skillMap map[string]string)
	DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (ReasoningResult, error)
}

type ResponseMeta struct {
	Model         string
	ResponseID    string
	InputTokens   int64
	OutputTokens  int64
	StopReason    string
	AssistantText string
}

type CompressionInfo struct {
	MessagesBefore int
	MessagesAfter  int
	Summary        string
}

type ReasoningResult struct {
	ToolCall    *tooldef.ToolCall
	FinalAnswer string
	Meta        ResponseMeta
	Compression *CompressionInfo
}

func (r ReasoningResult) String() string {
	var metadataBuilder strings.Builder
	if r.ToolCall != nil {
		name := r.ToolCall.Name
		input := r.ToolCall.Input
		fmt.Fprintf(&metadataBuilder, "name:%s; input:%s;", name, input)
	}
	if r.FinalAnswer != "" {
		fmt.Fprintf(&metadataBuilder, "finalAnswer:%s;", r.FinalAnswer)
	}
	return metadataBuilder.String()
}
