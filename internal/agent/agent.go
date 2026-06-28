package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	agentctx "tenzing-agent/internal/agent/context"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

var _ runner.Agent = (*Agent)(nil)

type Agent struct {
	model        provider.LLM
	systemPrompt string
	history      *agentctx.Context

	skillMap         map[string]string
	tools            []provider.ToolDefinition
	streamCallback   func(text string)
	thinkingCallback func(text string)
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
	var systemPrompt strings.Builder
	systemPrompt.WriteString(prompt)
	if len(skillMap) > 0 {
		systemPrompt.WriteString("\n\nAvailable skills (call load_skill to get full instructions):")
		for name, desc := range skillMap {
			fmt.Fprintf(&systemPrompt, "\n- %s: %s", name, desc)
		}
		systemPrompt.WriteString("\nWhen a task requires specialised knowledge, call load_skill(name) to get full instructions before starting. Do NOT guess.")
	}
	return systemPrompt.String()
}

func (a *Agent) UpdateSkillMap(skillMap map[string]string) {
	a.skillMap = skillMap
}

func (a *Agent) UpdateOffloadFn(fn func(ctx context.Context, input string) (string, error)) {
	a.history.UpdateOffloadFn(fn)
}

func (a *Agent) SetTodoProvider(fn func() string) {
	a.history.SetTodoProvider(fn)
}

func (a *Agent) UpdateStreamCallback(fn func(text string)) {
	a.streamCallback = fn
}

func (a *Agent) UpdateThinkingCallback(fn func(text string)) {
	a.thinkingCallback = fn
}

func (a *Agent) UpdateToolDefinitions(tooldefs []provider.ToolDefinition) {
	a.tools = tooldefs
}

// replaceInput replaces the string at index "idx" for "inputs" array
func replaceInput(inputs []string, idx int, replacement string) []string {
	out := make([]string, len(inputs))
	copy(out, inputs)
	out[idx] = replacement
	return out
}

func (a *Agent) doStreamingReasoning(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	req.Tools = a.tools
	events := make(chan provider.StreamEvent)

	var streamErr error
	done := make(chan struct{})
	go func() {
		streamErr = a.model.SendStreamingMessage(ctx, req, events)
		close(done)
	}()

	var resp provider.CompletionResponse
	for event := range events {
		switch event.Type {
		case provider.StreamEventDelta:
			a.streamCallback(event.Text)
		case provider.StreamEventThinking:
			if a.thinkingCallback != nil {
				a.thinkingCallback(event.Text)
			}
		case provider.StreamEventStop:
			if event.Response != nil {
				resp = *event.Response
			}
		case provider.StreamEventError:
			return provider.CompletionResponse{}, event.Err
		}
	}

	<-done
	if streamErr != nil {
		return provider.CompletionResponse{}, streamErr
	}
	return resp, nil
}

func (a *Agent) DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (runner.ReasoningResult, error) {
	// if the inputs + context will blow the context limits, see if we can move to RLM
	result, idx, err := a.history.ClassifyOverflow(ctx, inputs)
	if err != nil {
		slog.Warn("[offload] failed, using original input", "error", err)
	}
	if result != "" {
		slog.Info("[offload] complete", "original_len", len(inputs[idx]), "result_len", len(result))
		inputs = replaceInput(inputs, idx, "[RLM processed result]\n\n"+result)
	}

	// convert inputs arg to user messages and add to context
	userMsgs := make([]provider.Message, len(inputs))
	for i, input := range inputs {
		userMsgs[i] = provider.NewUserMessage(input)
	}
	if _, err := a.history.AppendMessages(ctx, userMsgs...); err != nil {
		return runner.ReasoningResult{}, fmt.Errorf("append user inputs: %w", err)
	}

	// add system reminders to system prompt
	system := a.systemPrompt
	for _, r := range systemReminders {
		system += "\n\n" + r
	}

	// create LLM request
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
	if slog.Default().Enabled(ctx, runner.LevelTrace) {
		slog.Log(ctx, runner.LevelTrace, "llm request system prompt", "model", model, "system", system)
		if raw, err := json.Marshal(messages); err == nil {
			slog.Log(ctx, runner.LevelTrace, "llm request messages", "model", model, "messages_json", string(raw))
		}
		if raw, err := json.Marshal(a.tools); err == nil {
			slog.Log(ctx, runner.LevelTrace, "llm request tools", "model", model, "tools_json", string(raw))
		}
	}

	// get the LLM response
	var resp provider.CompletionResponse
	if a.streamCallback != nil {
		resp, err = a.doStreamingReasoning(ctx, req)
	} else {
		resp, err = a.model.SendMessageWithTools(ctx, req, a.tools)
	}
	if err != nil {
		slog.Error("llm call failed", "model", model, "error", err, "messages", a.history.Len(), "stack", string(debug.Stack()))
		return runner.ReasoningResult{}, fmt.Errorf("llm call (%s): %w", model, err)
	}

	slog.Info("llm response", "model", resp.Model, "response_id", resp.ID, "input_tokens", resp.Usage.InputTokens, "output_tokens", resp.Usage.OutputTokens, "stop_reason", resp.StopReason)
	if text := resp.Text(); text != "" {
		slog.Debug("assistant text", "text", text)
	}

	// add response as assistant message and add to context
	assistantMsg := provider.Message{
		Role:    provider.RoleAssistant,
		Content: resp.Content,
	}
	beforeCount := a.history.Len()
	compressed, err := a.history.AppendMessages(ctx, assistantMsg)
	if err != nil {
		slog.Warn("compression failed", "error", err)
	}

	// check if context was compressed
	var compressionInfo *runner.CompressionInfo
	if compressed {
		afterCount := a.history.Len()
		slog.Info("context compressed", "before_msgs", beforeCount+1, "after_msgs", afterCount)
		compressionInfo = &runner.CompressionInfo{
			MessagesBefore: beforeCount + 1,
			MessagesAfter:  afterCount,
		}
	}

	// get the response details for logging
	meta := runner.ResponseMeta{
		Model:         resp.Model,
		ResponseID:    resp.ID,
		InputTokens:   resp.Usage.InputTokens,
		OutputTokens:  resp.Usage.OutputTokens,
		StopReason:    string(resp.StopReason),
		AssistantText: resp.Text(),
	}

	// if the action to take is a tool call, get it
	toolCalls := resp.ToolCalls()
	if len(toolCalls) > 0 {
		tc := toolCalls[0]
		return runner.ReasoningResult{
			ToolCall: &tooldef.ToolCall{
				ID:    tc.ToolUseID,
				Name:  tc.ToolName,
				Input: string(tc.ToolInput),
			},
			Meta:        meta,
			Compression: compressionInfo,
		}, nil
	}

	// if there are no tool calls, then just return the response
	return runner.ReasoningResult{
		FinalAnswer: resp.Text(),
		Meta:        meta,
		Compression: compressionInfo,
	}, nil
}
