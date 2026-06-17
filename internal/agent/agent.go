package agent

import (
	"context"
	"fmt"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/provider"
	"tenzing-agent/internal/tools/tooldef"
)

type Agent struct {
	model        provider.LLM
	tools        []provider.ToolDefinition
	history      []provider.Message
	systemPrompt string
}

type AgentConfig struct {
	Model           provider.LLM
	ToolDefinitions []provider.ToolDefinition
	SystemPrompt    string
}

func New(cfg AgentConfig) *Agent {
	return &Agent{
		model:        cfg.Model,
		tools:        cfg.ToolDefinitions,
		systemPrompt: cfg.SystemPrompt,
		history:      make([]provider.Message, 0),
	}
}

func (a *Agent) DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (harness.ReasoningResult, error) {
	// Convert string inputs to messages and append to history
	for _, input := range inputs {
		a.history = append(a.history, provider.NewUserMessage(input))
	}

	// Build system prompt with reminders
	system := a.systemPrompt
	for _, r := range systemReminders {
		system += "\n\n" + r
	}

	req := provider.CompletionRequest{
		Model:     a.model.GetCurrentModel(),
		System:    system,
		Messages:  a.history,
		MaxTokens: provider.MaxTokensStdResponse,
		Tools:     a.tools,
	}

	resp, err := a.model.SendMessageWithTools(ctx, req, a.tools)
	if err != nil {
		return harness.ReasoningResult{}, fmt.Errorf("llm call: %w", err)
	}

	// Append assistant response to history
	a.history = append(a.history, provider.Message{
		Role:    provider.RoleAssistant,
		Content: resp.Content,
	})

	// Check for tool calls
	toolCalls := resp.ToolCalls()
	if len(toolCalls) > 0 {
		tc := toolCalls[0]
		return harness.ReasoningResult{
			ToolCall: &tooldef.ToolCall{
				Name:  tc.ToolName,
				Input: string(tc.ToolInput),
			},
		}, nil
	}

	// Final answer
	return harness.ReasoningResult{
		FinalAnswer: resp.Text(),
	}, nil
}
