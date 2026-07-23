package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"

	"github.com/tab58/llm-providers/common"
)

// maxTokensStdResponse caps output tokens per LLM request.
const maxTokensStdResponse int64 = 32768

var _ runner.Agent = (*Agent)(nil)

type Agent struct {
	model        common.LLM
	systemPrompt string

	streamCallback   func(text string)
	thinkingCallback func(text string)
}

type AgentConfig struct {
	Model        common.LLM
	SystemPrompt string
}

type agentOptions struct {
	systemPrompt string
}

type ConfigOption func(*agentOptions)

// WithSystemPrompt configures the Agent with a default system prompt
func WithSystemPrompt(prompt string) ConfigOption {
	return func(o *agentOptions) {
		if prompt != "" {
			o.systemPrompt = prompt
		}
	}
}

func New(cfg AgentConfig, opts ...ConfigOption) (*Agent, error) {
	o := &agentOptions{
		systemPrompt: "", // TODO: insert default system prompt?
	}
	for _, opt := range opts {
		opt(o)
	}

	return &Agent{
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
	}, nil
}

func (a *Agent) GetCurrentModel() string {
	return a.model.GetCurrentModel()
}

func (a *Agent) UpdateStreamCallback(fn func(text string)) {
	a.streamCallback = fn
}

func (a *Agent) UpdateThinkingCallback(fn func(text string)) {
	a.thinkingCallback = fn
}

func (a *Agent) doStreamingReasoning(ctx context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	events := make(chan common.StreamEvent)

	var streamErr error
	done := make(chan struct{})
	go func() {
		streamErr = a.model.SendStreamingMessage(ctx, req, events)
		close(done)
	}()

	var resp common.CompletionResponse
	for event := range events {
		switch event.Type {
		case common.StreamEventDelta:
			a.streamCallback(event.Text)
		case common.StreamEventThinking:
			if a.thinkingCallback != nil {
				a.thinkingCallback(event.Text)
			}
		case common.StreamEventStop:
			if event.Response != nil {
				resp = *event.Response
			}
		case common.StreamEventError:
			return common.CompletionResponse{}, event.Err
		}
	}

	<-done
	if streamErr != nil {
		return common.CompletionResponse{}, streamErr
	}
	return resp, nil
}

// DoReasoning is a single, stateless model call: messages is the full
// conversation history, built and owned by the caller's ContextPort, and
// tools is the tool surface for this turn, owned by the caller's ToolPort.
// The agent neither stores nor mutates them — the returned
// Meta.AssistantMessage carries the model's response back for the caller to
// append.
func (a *Agent) DoReasoning(ctx context.Context, messages []common.Message, systemReminders []string, tools []common.ToolDefinition) (runner.ReasoningResult, error) {
	// add system reminders to system prompt
	system := a.systemPrompt
	for _, r := range systemReminders {
		system += "\n\n" + r
	}

	// create LLM request
	model := a.model.GetCurrentModel()
	req := common.CompletionRequest{
		Model:     model,
		System:    system,
		Messages:  messages,
		MaxTokens: maxTokensStdResponse,
		Tools:     tools,
	}

	slog.Debug("llm request", "model", model, "messages", len(messages), "tools", len(tools))
	if slog.Default().Enabled(ctx, runner.LevelTrace) {
		slog.Log(ctx, runner.LevelTrace, "llm request system prompt", "model", model, "system", system)
		if raw, err := json.Marshal(messages); err == nil {
			slog.Log(ctx, runner.LevelTrace, "llm request messages", "model", model, "messages_json", string(raw))
		}
		if raw, err := json.Marshal(tools); err == nil {
			slog.Log(ctx, runner.LevelTrace, "llm request tools", "model", model, "tools_json", string(raw))
		}
	}

	// get the LLM response
	var resp common.CompletionResponse
	var err error
	if a.streamCallback != nil {
		resp, err = a.doStreamingReasoning(ctx, req)
	} else {
		resp, err = a.model.SendMessageWithTools(ctx, req, tools)
	}
	if err != nil {
		slog.Error("llm call failed", "model", model, "error", err, "messages", len(messages), "stack", string(debug.Stack()))
		return runner.ReasoningResult{}, fmt.Errorf("llm call (%s): %w", model, err)
	}

	slog.Info("llm response", "model", resp.Model, "response_id", resp.ID, "input_tokens", resp.Usage.InputTokens, "output_tokens", resp.Usage.OutputTokens, "stop_reason", resp.StopReason)
	if text := resp.Text(); text != "" {
		slog.Debug("assistant text", "text", text)
	}

	// get the response details for logging
	meta := runner.ResponseMeta{
		Model:            resp.Model,
		ResponseID:       resp.ID,
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		StopReason:       string(resp.StopReason),
		AssistantText:    resp.Text(),
		AssistantMessage: common.Message{Role: common.RoleAssistant, Content: resp.Content},
	}

	// if the action to take is tool calls, return all of them; the runner
	// executes each and feeds the results back in the same order.
	toolCalls := resp.ToolCalls()
	if len(toolCalls) > 0 {
		calls := make([]tooldef.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			calls[i] = tooldef.ToolCall{
				ID:    tc.ToolUseID,
				Name:  tc.ToolName,
				Input: string(tc.ToolInput),
			}
		}
		return runner.ReasoningResult{
			ToolCalls: calls,
			Meta:      meta,
		}, nil
	}

	// if there are no tool calls, then just return the response
	return runner.ReasoningResult{
		FinalAnswer: resp.Text(),
		Meta:        meta,
	}, nil
}
