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

var _ harness.Agent = (*Agent)(nil)

type Agent struct {
	model        provider.LLM
	systemPrompt string
	history      *agentctx.Context

	// the skill map definitions that get updated from the AgentRunner
	skillMap map[string]string

	// the tool definitions that gets updated from the AgentRunner
	tools []provider.ToolDefinition
}

type AgentConfig struct {
	Model        provider.LLM
	SystemPrompt string
	SkillMap     map[string]string
}

func New(cfg AgentConfig) (*Agent, error) {
	systemPrompt := cfg.SystemPrompt
	skillMap := cfg.SkillMap
	enrichedPrompt := buildAgentSystemPrompt(systemPrompt, skillMap)
	llmContext, err := agentctx.NewContext(agentctx.ContextConfig{LLM: cfg.Model})
	if err != nil {
		return nil, fmt.Errorf("create context: %w", err)
	}

	return &Agent{
		model:        cfg.Model,
		tools:        []provider.ToolDefinition{},
		skillMap:     skillMap,
		systemPrompt: enrichedPrompt,
		history:      llmContext,
	}, nil
}

func buildAgentSystemPrompt(prompt string, skillMap map[string]string) string {
	systemPrompt := prompt
	if len(skillMap) > 0 {
		systemPrompt += "\n\nAvailable skills (call load_skill to get full instructions):"
		for name, desc := range skillMap {
			systemPrompt += fmt.Sprintf("\n- %s: %s", name, desc)
		}
		systemPrompt += "\nWhen a task requires specialised knowledge, call load_skill(name) to get full instructions before starting. Do NOT guess."
	}
	return systemPrompt
}

func (a *Agent) UpdateSkillMap(skillMap map[string]string) {
	a.skillMap = skillMap
}

func (a *Agent) UpdateToolDefinitions(tooldefs []provider.ToolDefinition) {
	a.tools = tooldefs
}

func (a *Agent) DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (harness.ReasoningResult, error) {
	userMsgs := make([]provider.Message, len(inputs))
	for i, input := range inputs {
		userMsgs[i] = provider.NewUserMessage(input)
	}
	if _, err := a.history.AppendMessages(ctx, userMsgs...); err != nil {
		return harness.ReasoningResult{}, fmt.Errorf("append user inputs: %w", err)
	}

	system := a.systemPrompt
	for _, r := range systemReminders {
		system += "\n\n" + r
	}

	messages := a.history.Messages()
	model := a.model.GetCurrentModel()
	req := provider.CompletionRequest{
		Model:     model,
		System:    system,
		Messages:  messages,
		MaxTokens: provider.MaxTokensStdResponse,
		Tools:     a.tools,
	}

	slog.Debug("llm request", "model", model, "messages", a.history.Len(), "tools", len(a.tools))

	if slog.Default().Enabled(ctx, harness.LevelTrace) {
		slog.Log(ctx, harness.LevelTrace, "llm request system prompt", "model", model, "system", system)
		if raw, err := json.Marshal(messages); err == nil {
			slog.Log(ctx, harness.LevelTrace, "llm request messages", "model", model, "messages_json", string(raw))
		}
		if raw, err := json.Marshal(a.tools); err == nil {
			slog.Log(ctx, harness.LevelTrace, "llm request tools", "model", model, "tools_json", string(raw))
		}
	}

	resp, err := a.model.SendMessageWithTools(ctx, req, a.tools)
	if err != nil {
		slog.Error("llm call failed", "model", model, "error", err, "messages", a.history.Len(), "stack", string(debug.Stack()))
		return harness.ReasoningResult{}, fmt.Errorf("llm call (%s): %w", model, err)
	}

	slog.Info("llm response", "model", resp.Model, "response_id", resp.ID, "input_tokens", resp.Usage.InputTokens, "output_tokens", resp.Usage.OutputTokens, "stop_reason", resp.StopReason)

	if text := resp.Text(); text != "" {
		slog.Debug("assistant text", "text", text)
	}

	assistantMsg := provider.Message{
		Role:    provider.RoleAssistant,
		Content: resp.Content,
	}
	beforeCount := a.history.Len()
	compressed, err := a.history.AppendMessages(ctx, assistantMsg)
	if err != nil {
		slog.Warn("compression failed", "error", err)
	}

	var compressionInfo *harness.CompressionInfo
	if compressed {
		afterCount := a.history.Len()
		slog.Info("context compressed", "before_msgs", beforeCount+1, "after_msgs", afterCount)
		compressionInfo = &harness.CompressionInfo{
			MessagesBefore: beforeCount + 1,
			MessagesAfter:  afterCount,
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

	return harness.ReasoningResult{
		FinalAnswer: resp.Text(),
		Meta:        meta,
		Compression: compressionInfo,
	}, nil
}
