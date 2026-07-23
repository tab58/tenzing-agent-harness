package runner

import (
	"context"

	"github.com/tab58/tenzing-agent-harness/internal/core"

	"github.com/tab58/llm-providers/common"
)

// An Agent is an abstraction of the "brain" of the harness. Any decision making for the harness
// is executed from an Agent. Agents are stateless: DoReasoning receives the full message history
// on every call — the runner owns the conversation via a core.ContextPort.
type Agent interface {
	GetCurrentModel() string
	UpdateStreamCallback(fn func(text string))
	UpdateThinkingCallback(fn func(text string))
	DoReasoning(ctx context.Context, messages []common.Message, systemReminders []string, tools []common.ToolDefinition) (ReasoningResult, error)
}

// AgentBuilder creates an Agent given an LLM and system prompt.
type AgentBuilder func(llm common.LLM, systemPrompt string) (Agent, error)

// ResponseMeta and ReasoningResult live in core (core.ModelPort's return
// type); aliased here so existing callers of runner.ResponseMeta /
// runner.ReasoningResult keep compiling.
type (
	ResponseMeta    = core.ResponseMeta
	ReasoningResult = core.ReasoningResult
)
