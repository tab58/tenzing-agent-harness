package runner

import (
	"github.com/tab58/tenzing-agent-harness/internal/core"

	"github.com/tab58/llm-providers/common"
)

// AgentBuilder creates a core.Agent given an LLM and system prompt.
type AgentBuilder func(llm common.LLM, systemPrompt string) (core.Agent, error)
