package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
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
	a.compressor = agentctx.NewCompressor(cfg.Model, cwd+"/"+agentctx.MemoryFileName, cfg.Model.GetContextWindowSize())

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

	model := a.model.GetCurrentModel()
	req := provider.CompletionRequest{
		Model:     model,
		System:    system,
		Messages:  a.history,
		MaxTokens: provider.MaxTokensStdResponse,
		Tools:     a.tools,
	}

	slog.Debug("llm request", "model", model, "messages", len(a.history), "tools", len(a.tools))

	if slog.Default().Enabled(ctx, harness.LevelTrace) {
		slog.Log(ctx, harness.LevelTrace, "llm request system prompt", "model", model, "system", system)
		if raw, err := json.Marshal(a.history); err == nil {
			slog.Log(ctx, harness.LevelTrace, "llm request messages", "model", model, "messages_json", string(raw))
		}
		if raw, err := json.Marshal(a.tools); err == nil {
			slog.Log(ctx, harness.LevelTrace, "llm request tools", "model", model, "tools_json", string(raw))
		}
	}

	resp, err := a.model.SendMessageWithTools(ctx, req, a.tools)
	if err != nil {
		slog.Error("llm call failed", "model", model, "error", err, "messages", len(a.history), "stack", string(debug.Stack()))
		return harness.ReasoningResult{}, fmt.Errorf("llm call (%s): %w", model, err)
	}

	slog.Info("llm response", "model", resp.Model, "response_id", resp.ID, "input_tokens", resp.Usage.InputTokens, "output_tokens", resp.Usage.OutputTokens, "stop_reason", resp.StopReason)

	if text := resp.Text(); text != "" {
		slog.Debug("assistant text", "text", text)
	}

	// Append assistant response to history
	a.history = append(a.history, provider.Message{
		Role:    provider.RoleAssistant,
		Content: resp.Content,
	})

	var compressionInfo *harness.CompressionInfo
	if a.compressor != nil {
		estimatedSize := a.compressor.EstimateSize(a.history)
		beforeCount := len(a.history)
		compressed, did, compErr := a.compressor.MaybeCompress(ctx, a.history)
		if compErr != nil {
			slog.Warn("compression failed", "error", compErr)
		} else if did {
			slog.Info("context compressed", "before_msgs", beforeCount, "after_msgs", len(compressed), "estimated_chars", estimatedSize)
			var summary string
			if len(compressed) > 0 && len(compressed[0].Content) > 0 {
				summary = compressed[0].Content[0].Text
				if len(summary) > 500 {
					slog.Debug("compression summary", "summary", summary[:500]+"...[truncated]")
				} else {
					slog.Debug("compression summary", "summary", summary)
				}
			}
			compressionInfo = &harness.CompressionInfo{
				MessagesBefore: beforeCount,
				MessagesAfter:  len(compressed),
				Summary:        summary,
			}
			a.history = compressed
		}
	}

	meta := harness.ResponseMeta{
		Model:         resp.Model,
		ResponseID:    resp.ID,
		InputTokens:   resp.Usage.InputTokens,
		OutputTokens:  resp.Usage.OutputTokens,
		StopReason:    string(resp.StopReason),
		AssistantText: resp.Text(),
	}

	// Check for tool calls
	toolCalls := resp.ToolCalls()
	if len(toolCalls) > 0 {
		tc := toolCalls[0]
		return harness.ReasoningResult{
			ToolCall: &tooldef.ToolCall{
				ID:    tc.ToolUseID,
				Name:  tc.ToolName,
				Input: string(tc.ToolInput),
			},
			Meta:        meta,
			Compression: compressionInfo,
		}, nil
	}

	// Final answer
	return harness.ReasoningResult{
		FinalAnswer: resp.Text(),
		Meta:        meta,
		Compression: compressionInfo,
	}, nil
}
