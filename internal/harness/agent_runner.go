package harness

import (
	"context"
	"fmt"
	"log/slog"
	"tenzing-agent/internal/tools"
	"tenzing-agent/internal/tools/tooldef"
)

type AgentRunner struct {
	agent        Agent
	fsm          *LoopFSM
	toolRegistry *tools.Registry
	hooks        Hooks
	cwd          string
	systemPrompt string
}

type AgentRunnerConfig struct {
	Agent        Agent
	ToolRegistry *tools.Registry
	Hooks        Hooks
	Cwd          string
	SystemPrompt string
}

func NewAgentRunner(cfg AgentRunnerConfig) *AgentRunner {
	return &AgentRunner{
		agent:        cfg.Agent,
		fsm:          createNewLoopFSM(),
		toolRegistry: cfg.ToolRegistry,
		hooks:        cfg.Hooks,
		cwd:          cfg.Cwd,
		systemPrompt: cfg.SystemPrompt,
	}
}

// RunTaskLoop executes a single turn: user input -> agent plan/execute loop -> agent result
func (h *AgentRunner) RunLoop(ctx context.Context, input string) (string, error) {
	inputs := []string{input}
	var loopErr error

	if err := h.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		return "", fmt.Errorf("fsm reset: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		if err := h.fsm.TransitionStates(ctx, LoopTransitionStartReasoning); err != nil {
			loopErr = fmt.Errorf("fsm start reasoning: %w", err)
			break
		}
		reminders := h.buildSystemReminders()
		reasoningResult, err := h.agent.DoReasoning(inputs, reminders)
		if err != nil {
			loopErr = fmt.Errorf("reasoning error: %w", err)
			break
		}
		if err := h.fsm.TransitionStates(ctx, LoopTransitionFinishReasoning); err != nil {
			loopErr = fmt.Errorf("fsm finish reasoning: %w", err)
			break
		}

		if reasoningResult.ToolCall == nil {
			slog.Debug("tool not needed; returning final answer")
			finalAnswer := reasoningResult.FinalAnswer
			slog.Debug(fmt.Sprintf("final answer: %s", finalAnswer))
			if err := h.fsm.TransitionStates(ctx, LoopTransitionStop); err != nil {
				return "", fmt.Errorf("fsm stop: %w", err)
			}
			return finalAnswer, nil
		}

		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		if err := h.fsm.TransitionStates(ctx, LoopTransitionStartToolExecution); err != nil {
			loopErr = fmt.Errorf("fsm start tool execution: %w", err)
			break
		}
		toolCall := reasoningResult.ToolCall
		exctx := tooldef.ExecutionContext{
			Arguments:  []string{toolCall.Input},
			WorkingDir: h.cwd,
		}
		toolResult, err := h.toolRegistry.Execute(ctx, toolCall.Name, exctx)
		if err != nil {
			loopErr = fmt.Errorf("tool execution error: %w", err)
			break
		}
		if err := h.fsm.TransitionStates(ctx, LoopTransitionFinishToolExecution); err != nil {
			loopErr = fmt.Errorf("fsm finish tool execution: %w", err)
			break
		}
		slog.Debug(fmt.Sprintf("tool execution result: %s", toolResult.Output))

		// send out the data from the tool call
		if h.hooks.OnToolCall != nil {
			h.hooks.OnToolCall(toolCall.Name, toolCall.Input, toolResult.Output)
		}

		// Loop: feed tool result back to agent for next reasoning cycle
		inputs = append(inputs, toolResult.Output)

		if reminder := tooldef.ReadTodoReminder(h.cwd); reminder != "" {
			inputs = append(inputs, reminder)
		}
	}

	if err := h.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		slog.Error(fmt.Sprintf("fsm reset after error: %v", err))
	}
	slog.Error(loopErr.Error())
	return "", loopErr
}

func (h *AgentRunner) SystemPrompt() string {
	return h.systemPrompt
}

func (h *AgentRunner) buildSystemReminders() []string {
	var reminders []string
	if r := tooldef.ReadTodoReminder(h.cwd); r != "" {
		reminders = append(reminders, r)
	}
	return reminders
}
