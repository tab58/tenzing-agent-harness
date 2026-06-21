package agent

import (
	"context"
	"fmt"
	"log/slog"
	agentctx "tenzing-agent/internal/agent/context"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

type Agent struct {
	model        provider.LLM
	tools        []provider.ToolDefinition
	history      []provider.Message
	systemPrompt string
	compressor   *agentctx.Compressor
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

func NewWithCompressor(cfg AgentConfig, cwd string) *Agent {
	a := New(cfg)
	a.compressor = agentctx.NewCompressor(cfg.Model, cwd+"/"+agentctx.MemoryFileName)

	if mem, err := a.compressor.LoadMemory(); err == nil && mem != "" {
		a.history = append(a.history,
			provider.NewUserMessage("[Context summary from previous conversation]\n\n"+mem),
			provider.NewAssistantMessage("Understood. I have the full context from our previous work."),
		)
	}

	return a
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

	if a.compressor != nil {
		compressed, did, compErr := a.compressor.MaybeCompress(ctx, a.history)
		if compErr != nil {
			slog.Warn("compression failed", "error", compErr)
		} else if did {
			slog.Info("context compressed", "before", len(a.history), "after", len(compressed))
			a.history = compressed
		}
	}

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
