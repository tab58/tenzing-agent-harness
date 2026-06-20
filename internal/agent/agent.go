package agent

import (
	"context"
	"fmt"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
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
	Skills          map[string]string // name -> description, injected into system prompt
}

func New(cfg AgentConfig) *Agent {
	systemPrompt := cfg.SystemPrompt
	if len(cfg.Skills) > 0 {
		systemPrompt += "\n\nAvailable skills (call load_skill to get full instructions):"
		for name, desc := range cfg.Skills {
			systemPrompt += fmt.Sprintf("\n- %s: %s", name, desc)
		}
		systemPrompt += "\nWhen a task requires specialised knowledge, call load_skill(name) to get full instructions before starting. Do NOT guess."
	}

	return &Agent{
		model:        cfg.Model,
		tools:        cfg.ToolDefinitions,
		systemPrompt: systemPrompt,
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
