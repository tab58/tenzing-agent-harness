package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"

	"github.com/tab58/llm-providers/common"
)

// An Agent is an abstraction of the "brain" of the harness. Any decision making for the harness
// is executed from an Agent.
type Agent interface {
	GetCurrentModel() string
	UpdateToolDefinitions(tooldefs []common.ToolDefinition)
	UpdateSkillMap(skillMap map[string]string)
	UpdateStreamCallback(fn func(text string))
	UpdateThinkingCallback(fn func(text string))
	SetTodoProvider(fn func() string)
	DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (ReasoningResult, error)
}

// AgentBuilder creates an Agent given an LLM and system prompt.
type AgentBuilder func(llm common.LLM, systemPrompt string) (Agent, error)

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
	// ToolCalls holds every tool_use block from the response, in order. The
	// runner must execute all of them and feed the results back in the same
	// order so each pairs with its tool_use id.
	ToolCalls   []tooldef.ToolCall
	FinalAnswer string
	Meta        ResponseMeta
	Compression *CompressionInfo
}

func (r ReasoningResult) String() string {
	var metadataBuilder strings.Builder
	for _, tc := range r.ToolCalls {
		fmt.Fprintf(&metadataBuilder, "name:%s; input:%s;", tc.Name, tc.Input)
	}
	if r.FinalAnswer != "" {
		fmt.Fprintf(&metadataBuilder, "finalAnswer:%s;", r.FinalAnswer)
	}
	return metadataBuilder.String()
}
