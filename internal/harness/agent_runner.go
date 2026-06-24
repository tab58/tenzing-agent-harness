package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/tools"
)

const logOutputMaxLen = 2000

type AgentRunner struct {
	id             string
	agent          Agent
	fsm            *LoopFSM
	toolRegistry   *tools.Registry
	skillsRegistry *skills.Registry
	hooks          Hooks
	systemPrompt   string
	buildReminders ReminderBuilder
}

type AgentRunnerConfig struct {
	Agent          Agent
	ToolRegistry   *tools.Registry
	SkillsRegistry *skills.Registry
	Hooks          Hooks
	SystemPrompt   string
	BuildReminders ReminderBuilder
}

func NewAgentRunner(cfg AgentRunnerConfig) *AgentRunner {
	return &AgentRunner{
		id:             runnerID(),
		agent:          cfg.Agent,
		fsm:            createNewLoopFSM(),
		toolRegistry:   cfg.ToolRegistry,
		hooks:          cfg.Hooks,
		systemPrompt:   cfg.SystemPrompt,
		buildReminders: cfg.BuildReminders,
	}
}

func runnerID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RunLoop executes a single turn: user input -> agent plan/execute loop -> agent result
func (h *AgentRunner) RunLoop(ctx context.Context, input string) (string, error) {
	inputs := []string{input}
	var loopErr error
	iteration := 0
	loopStart := time.Now()
	slog.Info("loop started", "runner", h.id, "input", input)
	slog.Debug("system prompt", "runner", h.id, "prompt_len", len(h.systemPrompt), "prompt", h.systemPrompt)

	if err := h.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		return "", fmt.Errorf("fsm reset: %w", err)
	}
	for {
		iteration++
		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		if err := h.fsm.TransitionStates(ctx, LoopTransitionStartReasoning); err != nil {
			loopErr = fmt.Errorf("fsm start reasoning: %w", err)
			break
		}
		reminders := h.buildSystemReminders()
		if len(reminders) > 0 {
			slog.Debug("system reminders", "runner", h.id, "iter", iteration, "count", len(reminders), "reminders", reminders)
		}
		reasoningResult, err := h.agent.DoReasoning(ctx, inputs, reminders)
		if err != nil {
			loopErr = fmt.Errorf("reasoning error: %w", err)
			break
		}

		if reasoningResult.ToolCall != nil {
			slog.Debug("reasoning result", "runner", h.id, "iter", iteration, "tool", reasoningResult.ToolCall.Name, "tool_use_id", reasoningResult.ToolCall.ID, "input", reasoningResult.ToolCall.Input)
		} else {
			slog.Debug("reasoning result", "runner", h.id, "iter", iteration, "final_answer_len", len(reasoningResult.FinalAnswer))
		}
		if err := h.fsm.TransitionStates(ctx, LoopTransitionFinishReasoning); err != nil {
			loopErr = fmt.Errorf("fsm finish reasoning: %w", err)
			break
		}

		if reasoningResult.ToolCall == nil {
			finalAnswer := reasoningResult.FinalAnswer
			if err := h.fsm.TransitionStates(ctx, LoopTransitionStop); err != nil {
				return "", fmt.Errorf("fsm stop: %w", err)
			}
			slog.Info("loop completed", "runner", h.id, "iterations", iteration, "duration", time.Since(loopStart).Round(time.Millisecond), "answer_len", len(finalAnswer))
			slog.Debug("final answer", "runner", h.id, "answer", finalAnswer)
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
		toolStart := time.Now()
		toolResult, err := h.toolRegistry.Execute(ctx, toolCall.Name, toolCall.Input)
		toolDuration := time.Since(toolStart)
		if err != nil {
			loopErr = fmt.Errorf("tool execution error: %w", err)
			break
		}
		if err := h.fsm.TransitionStates(ctx, LoopTransitionFinishToolExecution); err != nil {
			loopErr = fmt.Errorf("fsm finish tool execution: %w", err)
			break
		}
		if toolResult.IsError {
			slog.Warn("tool error", "runner", h.id, "iter", iteration, "tool", toolCall.Name, "output", truncateLog(toolResult.Output, logOutputMaxLen))
		}
		slog.Debug("tool result", "runner", h.id, "iter", iteration, "tool", toolCall.Name, "is_error", toolResult.IsError, "duration", toolDuration.Round(time.Millisecond), "output_len", len(toolResult.Output), "output", truncateLog(toolResult.Output, logOutputMaxLen))
		slog.Log(ctx, LevelTrace, "tool result full", "runner", h.id, "iter", iteration, "tool", toolCall.Name, "output", toolResult.Output)

		if h.hooks.OnToolCall != nil {
			h.hooks.OnToolCall(toolCall.Name, toolCall.Input, toolResult.Output)
		}

		// Loop: feed tool result back to agent for next reasoning cycle
		inputs = append(inputs, toolResult.Output)
	}

	if err := h.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		slog.Error("fsm reset after error", "runner", h.id, "error", err)
	}
	slog.Error("loop failed", "runner", h.id, "error", loopErr, "iterations", iteration, "duration", time.Since(loopStart).Round(time.Millisecond))
	return "", loopErr
}

func (h *AgentRunner) ID() string {
	return h.id
}

func (h *AgentRunner) SystemPrompt() string {
	return h.systemPrompt
}

func (h *AgentRunner) buildSystemReminders() []string {
	if h.buildReminders == nil {
		return nil
	}
	return h.buildReminders()
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
