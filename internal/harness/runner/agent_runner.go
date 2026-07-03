package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
)

const logOutputMaxLen = 2000

type AgentRunner struct {
	id              string
	fsm             *LoopFSM
	toolRegistry    *tools.Registry
	skillsRegistry  *skills.Registry
	todoFile        *todo.TodoFile
	agent           Agent
	emitter         events.Emitter
	onTextDelta     func(string)
	onThinkingDelta func(string)
	systemPrompt    string
}

type AgentRunnerConfig struct {
	ToolRegistry   *tools.Registry
	SkillsRegistry *skills.Registry
	TodoFile       *todo.TodoFile

	Agent           Agent
	Emitter         events.Emitter
	OnTextDelta     func(string)
	OnThinkingDelta func(string)
	SystemPrompt    string
}

func NewAgentRunner(cfg AgentRunnerConfig) (*AgentRunner, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("no agent defined")
	}

	return &AgentRunner{
		id:              runnerID(),
		agent:           cfg.Agent,
		fsm:             createNewLoopFSM(),
		toolRegistry:    cfg.ToolRegistry,
		skillsRegistry:  cfg.SkillsRegistry,
		emitter:         cfg.Emitter,
		onTextDelta:     cfg.OnTextDelta,
		onThinkingDelta: cfg.OnThinkingDelta,
		systemPrompt:    cfg.SystemPrompt,
		todoFile:        cfg.TodoFile,
	}, nil
}

func (h *AgentRunner) emit(e events.Event) {
	if h.emitter != nil {
		h.emitter.Emit(e)
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

	// prepare loop
	h.agent.UpdateSkillMap(h.skillsRegistry.GetSkillMap())
	h.agent.UpdateToolDefinitions(h.toolRegistry.ProviderDefinitions())

	// execute loop
	slog.Info("loop started", "runner", h.id, "input", input)
	slog.Debug("system prompt", "runner", h.id, "prompt_len", len(h.systemPrompt), "prompt", h.systemPrompt)
	h.emit(events.TurnStartedEvent{
		BaseEvent: events.NewBaseEvent(events.EventTurnStarted, h.id),
		Query:     input,
	})
	h.emit(events.LoopStartedEvent{
		BaseEvent: events.NewBaseEvent(events.EventLoopStarted, h.id),
		Input:     input,
	})
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
		if h.onTextDelta != nil {
			h.agent.UpdateStreamCallback(h.onTextDelta)
		} else {
			h.agent.UpdateStreamCallback(nil)
		}
		if h.onThinkingDelta != nil {
			h.agent.UpdateThinkingCallback(h.onThinkingDelta)
		} else {
			h.agent.UpdateThinkingCallback(nil)
		}
		h.emit(events.ReasoningStartedEvent{
			BaseEvent: events.NewBaseEvent(events.EventReasoningStarted, h.id),
			Iteration: iteration,
		})
		reasoningResult, err := h.agent.DoReasoning(ctx, inputs, reminders)
		if err != nil {
			loopErr = fmt.Errorf("reasoning error: %w", err)
			break
		}
		h.emit(events.ReasoningFinishedEvent{
			BaseEvent:    events.NewBaseEvent(events.EventReasoningFinished, h.id),
			Model:        reasoningResult.Meta.Model,
			InputTokens:  reasoningResult.Meta.InputTokens,
			OutputTokens: reasoningResult.Meta.OutputTokens,
			StopReason:   reasoningResult.Meta.StopReason,
			HasToolCall:  reasoningResult.ToolCall != nil,
		})
		h.emit(events.LLMResponseEvent{
			BaseEvent:    events.NewBaseEvent(events.EventLLMResponse, h.id),
			Model:        reasoningResult.Meta.Model,
			ResponseID:   reasoningResult.Meta.ResponseID,
			InputTokens:  reasoningResult.Meta.InputTokens,
			OutputTokens: reasoningResult.Meta.OutputTokens,
			StopReason:   reasoningResult.Meta.StopReason,
			Text:         reasoningResult.Meta.AssistantText,
		})
		if reasoningResult.Compression != nil {
			h.emit(events.ContextCompressedEvent{
				BaseEvent:      events.NewBaseEvent(events.EventContextCompressed, h.id),
				MessagesBefore: reasoningResult.Compression.MessagesBefore,
				MessagesAfter:  reasoningResult.Compression.MessagesAfter,
				Summary:        reasoningResult.Compression.Summary,
			})
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
			h.emit(events.LoopStoppedEvent{
				BaseEvent:  events.NewBaseEvent(events.EventLoopStopped, h.id),
				Iterations: iteration,
				Duration:   time.Since(loopStart).Round(time.Millisecond),
			})
			h.emit(events.TurnCompletedEvent{
				BaseEvent:   events.NewBaseEvent(events.EventTurnCompleted, h.id),
				FinalAnswer: finalAnswer,
				Iterations:  iteration,
				Duration:    time.Since(loopStart).Round(time.Millisecond),
			})
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
		h.emit(events.ToolExecutionStartedEvent{
			BaseEvent: events.NewBaseEvent(events.EventToolExecutionStarted, h.id),
			ToolName:  toolCall.Name,
			Input:     toolCall.Input,
		})
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

		h.emit(events.ToolExecutionFinishedEvent{
			BaseEvent: events.NewBaseEvent(events.EventToolExecutionFinished, h.id),
			ToolName:  toolCall.Name,
			Duration:  toolDuration.Round(time.Millisecond),
		})
		if toolResult.IsError {
			h.emit(events.ToolFailedEvent{
				BaseEvent: events.NewBaseEvent(events.EventToolFailed, h.id),
				ToolName:  toolCall.Name,
				Input:     toolCall.Input,
				Error:     toolResult.Output,
				Duration:  toolDuration.Round(time.Millisecond),
			})
		} else {
			h.emit(events.ToolSucceededEvent{
				BaseEvent: events.NewBaseEvent(events.EventToolSucceeded, h.id),
				ToolName:  toolCall.Name,
				Input:     toolCall.Input,
				Output:    toolResult.Output,
				Duration:  toolDuration.Round(time.Millisecond),
			})
		}

		// Loop: feed only the new tool result to the next reasoning cycle.
		// The agent keeps its own history; re-sending earlier inputs would
		// duplicate them in the context every iteration.
		inputs = []string{toolResult.Output}
	}

	if err := h.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		slog.Error("fsm reset after error", "runner", h.id, "error", err)
	}
	slog.Error("loop failed", "runner", h.id, "error", loopErr, "iterations", iteration, "duration", time.Since(loopStart).Round(time.Millisecond))
	h.emit(events.ErrorEvent{
		BaseEvent: events.NewBaseEvent(events.EventError, h.id),
		Error:     loopErr.Error(),
		Context:   "loop",
	})
	return "", loopErr
}

func (h *AgentRunner) ID() string {
	return h.id
}

func (h *AgentRunner) SystemPrompt() string {
	return h.systemPrompt
}

func (h *AgentRunner) buildSystemReminders() []string {
	var reminders []string
	if r := h.todoFile.FormatReminder(); r != "" {
		reminders = append(reminders, r)
	}
	return reminders
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
