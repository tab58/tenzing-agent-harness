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

	"github.com/tab58/llm-providers/common"
)

// maxTokensStdResponse caps output tokens per LLM request.
const maxTokensStdResponse int64 = 32768

var _ runner.Agent = (*Agent)(nil)

type Agent struct {
	model        common.LLM
	systemPrompt string
	history      *agentctx.Context

	skillMap         map[string]string
	tools            []common.ToolDefinition
	streamCallback   func(text string)
	thinkingCallback func(text string)

	// pendingToolUses holds the tool_use blocks from the last assistant
	// response. The next DoReasoning call pairs incoming inputs with these
	// ids as tool_result blocks — required by the Anthropic API, which
	// rejects histories where a tool_use is not answered by a tool_result
	// in the immediately following user message.
	pendingToolUses []common.ContentBlock
}

type AgentConfig struct {
	Model        common.LLM
	SystemPrompt string
	SkillMap     map[string]string
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

	systemPrompt := cfg.SystemPrompt
	skillMap := cfg.SkillMap
	enrichedPrompt := buildAgentSystemPrompt(systemPrompt, skillMap)
	llmContext, err := agentctx.NewContext(agentctx.ContextConfig{LLM: cfg.Model})
	if err != nil {
		return nil, fmt.Errorf("create context: %w", err)
	}

	return &Agent{
		model:        cfg.Model,
		tools:        []common.ToolDefinition{},
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

func (a *Agent) GetCurrentModel() string {
	return a.model.GetCurrentModel()
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

func (a *Agent) UpdateToolDefinitions(tooldefs []common.ToolDefinition) {
	a.tools = tooldefs
}

// replaceInput replaces the string at index "idx" for "inputs" array
func replaceInput(inputs []string, idx int, replacement string) []string {
	out := make([]string, len(inputs))
	copy(out, inputs)
	out[idx] = replacement
	return out
}

func (a *Agent) doStreamingReasoning(ctx context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	req.Tools = a.tools
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

	// convert inputs arg to user messages and add to context. When the
	// previous response contained tool_use blocks, inputs are tool outputs
	// and must be sent back as tool_result blocks paired by id.
	var userMsgs []common.Message
	if len(a.pendingToolUses) > 0 {
		blocks := make([]common.ContentBlock, 0, len(a.pendingToolUses))
		for i, tu := range a.pendingToolUses {
			output := "tool call was not executed"
			if i < len(inputs) {
				output = inputs[i]
			}
			blocks = append(blocks, common.NewToolResultContent(tu.ToolUseID, tu.ToolName, output))
		}
		// RoleTool: every provider converter renders this natively — the
		// Anthropic converter as a user message with tool_result blocks,
		// Ollama/OpenAI as role-"tool" messages. A plain RoleUser message
		// would drop the blocks in the Ollama/OpenAI text conversion.
		userMsgs = append(userMsgs, common.Message{Role: common.RoleTool, Content: blocks})
		for i := len(a.pendingToolUses); i < len(inputs); i++ {
			userMsgs = append(userMsgs, common.NewUserMessage(inputs[i]))
		}
		a.pendingToolUses = nil
	} else {
		userMsgs = make([]common.Message, len(inputs))
		for i, input := range inputs {
			userMsgs[i] = common.NewUserMessage(input)
		}
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
	req := common.CompletionRequest{
		Model:     model,
		System:    system,
		Messages:  messages,
		MaxTokens: maxTokensStdResponse,
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
	var resp common.CompletionResponse
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
	assistantMsg := common.Message{
		Role:    common.RoleAssistant,
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
		a.pendingToolUses = toolCalls
		// ponytail: only the first tool call is executed; the rest get
		// "not executed" tool_results and the model re-issues them.
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
