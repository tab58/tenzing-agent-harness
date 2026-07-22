package core

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/tab58/llm-providers/common"
)

const logOutputMaxLen = 2000

// maxInvalidFinalRetries bounds how many times an invalid final answer (empty,
// a tool call written as plain text, or a response truncated at the output
// token limit) is bounced back to the model before the loop gives up and
// returns it as-is.
const maxInvalidFinalRetries = 2

// toolCallTextRe matches text that is a malformed tool-call attempt emitted as
// prose — e.g. "<|tool_call>...", "call:graph_cypher{...}" — which smaller
// models produce under context pressure instead of a real tool invocation.
var toolCallTextRe = regexp.MustCompile(`(?s)^\s*(<\|+/?tool_call|call:[A-Za-z_]+\s*[{(])`)

// invalidFinalAnswerReason returns a non-empty reason when the answer should
// not be handed to the caller.
func invalidFinalAnswerReason(answer string) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return "the response was empty"
	}
	if toolCallTextRe.MatchString(trimmed) {
		return "the response looks like a tool call written as plain text"
	}
	return ""
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

// LoopConfig configures a Loop instance.
type LoopConfig struct {
	ID           string
	Model        ModelPort
	Tools        ToolPort
	Context      ContextPort
	Emitter      Emitter      // nil-safe
	Extensions   *Extensions  // nil → NewExtensions()
	SystemPrompt string       // exposed for logging; the ModelPort owns applying it
	FSM          *LoopFSM     // nil → NewLoopFSM()
}

// Loop is the invariant agent reasoning-tool execution loop. It owns the FSM,
// drives the ModelPort/ToolPort/ContextPort ports, and runs extension hooks.
type Loop struct {
	id         string
	model      ModelPort
	tools      ToolPort
	context    ContextPort
	emitter    Emitter
	extensions *Extensions
	sysPrompt  string
	fsm        *LoopFSM
}

// NewLoop creates a Loop from the given config.
func NewLoop(cfg LoopConfig) (*Loop, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("loop ID is required")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("ModelPort is required")
	}
	if cfg.Tools == nil {
		return nil, fmt.Errorf("ToolPort is required")
	}
	if cfg.Context == nil {
		return nil, fmt.Errorf("ContextPort is required")
	}
	if cfg.Extensions == nil {
		cfg.Extensions = NewExtensions()
	}
	if cfg.FSM == nil {
		cfg.FSM = NewLoopFSM()
	}
	return &Loop{
		id:         cfg.ID,
		model:      cfg.Model,
		tools:      cfg.Tools,
		context:    cfg.Context,
		emitter:    cfg.Emitter,
		extensions: cfg.Extensions,
		sysPrompt:  cfg.SystemPrompt,
		fsm:        cfg.FSM,
	}, nil
}

// ID returns the loop's identifier.
func (l *Loop) ID() string { return l.id }

func (l *Loop) emit(e Event) {
	if l.emitter != nil {
		l.emitter.Emit(e)
	}
}

// RunTurn executes a single turn: user input -> agent plan/execute loop -> agent result
func (l *Loop) RunTurn(ctx context.Context, input string) (TurnResult, error) {
	var loopErr error
	iteration := 0
	invalidFinalRetries := 0
	loopStart := time.Now()

	if err := l.context.AppendUser(ctx, input); err != nil {
		return TurnResult{}, fmt.Errorf("append input: %w", err)
	}

	// execute loop
	slog.Info("loop started", "runner", l.id, "input", input)
	slog.Debug("system prompt", "runner", l.id, "prompt_len", len(l.sysPrompt), "prompt", l.sysPrompt)
	l.emit(TurnStartedEvent{
		BaseEvent: NewBaseEvent(EventTurnStarted, l.id),
		Query:     input,
	})
	l.emit(LoopStartedEvent{
		BaseEvent: NewBaseEvent(EventLoopStarted, l.id),
		Input:     input,
	})
	if err := l.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		return TurnResult{}, fmt.Errorf("fsm reset: %w", err)
	}
	for {
		iteration++
		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		if err := l.fsm.TransitionStates(ctx, LoopTransitionStartReasoning); err != nil {
			loopErr = fmt.Errorf("fsm start reasoning: %w", err)
			break
		}
		turnCtx := &TurnContext{RunnerID: l.id, Iteration: iteration}
		if err := l.extensions.RunBeforeIteration(ctx, turnCtx); err != nil {
			loopErr = fmt.Errorf("before iteration hook: %w", err)
			break
		}
		if turnCtx.Terminate != "" {
			// graceful termination requested (budgets, Phase 5). Stop like a final answer.
			if err := l.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
				slog.Error("fsm reset after termination", "runner", l.id, "error", err)
			}
			slog.Info("loop terminated by hook", "runner", l.id, "reason", turnCtx.Terminate)
			return TurnResult{Terminated: turnCtx.Terminate}, nil
		}
		reminders := turnCtx.Reminders
		if len(reminders) > 0 {
			slog.Debug("system reminders", "runner", l.id, "iter", iteration, "count", len(reminders), "reminders", reminders)
		}
		l.emit(ReasoningStartedEvent{
			BaseEvent: NewBaseEvent(EventReasoningStarted, l.id),
			Iteration: iteration,
		})
		messages, err := l.context.Messages(ctx)
		if err != nil {
			loopErr = fmt.Errorf("read context: %w", err)
			break
		}
		reasoningResult, err := l.model.DoReasoning(ctx, messages, reminders)
		if err != nil {
			loopErr = fmt.Errorf("reasoning error: %w", err)
			break
		}
		l.emit(ReasoningFinishedEvent{
			BaseEvent:    NewBaseEvent(EventReasoningFinished, l.id),
			Model:        reasoningResult.Meta.Model,
			InputTokens:  reasoningResult.Meta.InputTokens,
			OutputTokens: reasoningResult.Meta.OutputTokens,
			StopReason:   reasoningResult.Meta.StopReason,
			HasToolCall:  len(reasoningResult.ToolCalls) > 0,
		})
		l.emit(LLMResponseEvent{
			BaseEvent:    NewBaseEvent(EventLLMResponse, l.id),
			Model:        reasoningResult.Meta.Model,
			ResponseID:   reasoningResult.Meta.ResponseID,
			InputTokens:  reasoningResult.Meta.InputTokens,
			OutputTokens: reasoningResult.Meta.OutputTokens,
			StopReason:   reasoningResult.Meta.StopReason,
			Text:         reasoningResult.Meta.AssistantText,
		})
		if len(reasoningResult.ToolCalls) > 0 {
			for _, tc := range reasoningResult.ToolCalls {
				slog.Debug("reasoning result", "runner", l.id, "iter", iteration, "tool", tc.Name, "tool_use_id", tc.ID, "input", tc.Input)
			}
		} else {
			slog.Debug("reasoning result", "runner", l.id, "iter", iteration, "final_answer_len", len(reasoningResult.FinalAnswer))
		}
		if err := l.fsm.TransitionStates(ctx, LoopTransitionFinishReasoning); err != nil {
			loopErr = fmt.Errorf("fsm finish reasoning: %w", err)
			break
		}

		if len(reasoningResult.ToolCalls) == 0 {
			if err := l.context.AppendAssistant(ctx, reasoningResult.Meta.AssistantMessage); err != nil {
				loopErr = fmt.Errorf("append assistant message: %w", err)
				break
			}
			finalAnswer := reasoningResult.FinalAnswer
			reason := invalidFinalAnswerReason(finalAnswer)
			truncated := reason == "" && reasoningResult.Meta.StopReason == string(common.StopReasonMaxTokens)
			if truncated {
				reason = "the response was cut off by the output token limit before it finished"
			}
			if reason != "" && invalidFinalRetries < maxInvalidFinalRetries {
				invalidFinalRetries++
				slog.Warn("invalid final answer, retrying", "runner", l.id, "iter", iteration, "reason", reason, "answer_len", len(finalAnswer), "retry", invalidFinalRetries)
				if err := l.fsm.TransitionStates(ctx, LoopTransitionStartToolExecution); err != nil {
					loopErr = fmt.Errorf("fsm start retry after invalid final answer: %w", err)
					break
				}
				if err := l.fsm.TransitionStates(ctx, LoopTransitionFinishToolExecution); err != nil {
					loopErr = fmt.Errorf("fsm finish retry after invalid final answer: %w", err)
					break
				}
				retryMsg := fmt.Sprintf(
					"Your previous response was rejected: %s. If you intended to call a tool, use the tool-calling mechanism — never write a tool call as text. Otherwise, reply with your final answer as plain prose.",
					reason)
				if truncated {
					retryMsg = "Your previous response was cut off by the output token limit before it finished. Write the complete final answer again from the start, more concisely, so it fits within the limit."
				}
				if err := l.context.AppendUser(ctx, retryMsg); err != nil {
					loopErr = fmt.Errorf("append retry message: %w", err)
					break
				}
				continue
			}
			if err := l.fsm.TransitionStates(ctx, LoopTransitionStop); err != nil {
				return TurnResult{}, fmt.Errorf("fsm stop: %w", err)
			}
			dur := time.Since(loopStart).Round(time.Millisecond)
			slog.Info("loop completed", "runner", l.id, "iterations", iteration, "duration", dur, "answer_len", len(finalAnswer))
			slog.Debug("final answer", "runner", l.id, "answer", finalAnswer)
			l.emit(LoopStoppedEvent{
				BaseEvent:  NewBaseEvent(EventLoopStopped, l.id),
				Iterations: iteration,
				Duration:   dur,
			})
			l.emit(TurnCompletedEvent{
				BaseEvent:   NewBaseEvent(EventTurnCompleted, l.id),
				FinalAnswer: finalAnswer,
				Iterations:  iteration,
				Duration:    dur,
			})
			tr := TurnResult{
				FinalAnswer: finalAnswer,
				Iterations:  iteration,
				Duration:    dur,
			}
			l.extensions.RunAfterTurn(ctx, &tr)
			return tr, nil
		}

		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		if err := l.fsm.TransitionStates(ctx, LoopTransitionStartToolExecution); err != nil {
			loopErr = fmt.Errorf("fsm start tool execution: %w", err)
			break
		}
		// Execute every tool call from the response, in order. Results are
		// fed back in the same order so each pairs with its tool_use id —
		// skipping any would poison the history with "not executed" results.
		outputs := make([]ToolResult, 0, len(reasoningResult.ToolCalls))
		for _, toolCall := range reasoningResult.ToolCalls {
			l.emit(ToolExecutionStartedEvent{
				BaseEvent: NewBaseEvent(EventToolExecutionStarted, l.id),
				ToolName:  toolCall.Name,
				Input:     toolCall.Input,
			})
			toolStart := time.Now()
			tcc := &ToolCallContext{RunnerID: l.id, Call: &toolCall, Origin: l.tools.Origin(toolCall.Name)}
			var toolResult ToolResult
			if hookErr := l.extensions.RunToolCall(ctx, tcc); hookErr != nil {
				toolResult = ToolResult{ToolUseID: toolCall.ID, Output: fmt.Sprintf("tool call blocked by extension: %v", hookErr), IsError: true}
			} else {
				switch tcc.Decision {
				case Deny:
					toolResult = ToolResult{ToolUseID: toolCall.ID, Output: fmt.Sprintf("tool call denied by policy: %s", tcc.Reason), IsError: true}
				case AskUser:
					// Approval flow arrives in Phase 5 (Task 16). Until then, unanswerable.
					toolResult = ToolResult{ToolUseID: toolCall.ID, Output: "tool call requires approval but no approver is configured", IsError: true}
				default:
					toolResult = l.tools.Execute(ctx, toolCall)
				}
			}
			toolDuration := time.Since(toolStart)
			if toolResult.IsError {
				slog.Warn("tool error", "runner", l.id, "iter", iteration, "tool", toolCall.Name, "output", truncateLog(toolResult.Output, logOutputMaxLen))
			}
			slog.Debug("tool result", "runner", l.id, "iter", iteration, "tool", toolCall.Name, "is_error", toolResult.IsError, "duration", toolDuration.Round(time.Millisecond), "output_len", len(toolResult.Output), "output", truncateLog(toolResult.Output, logOutputMaxLen))
			slog.Log(ctx, LevelTrace, "tool result full", "runner", l.id, "iter", iteration, "tool", toolCall.Name, "output", toolResult.Output)

			l.emit(ToolExecutionFinishedEvent{
				BaseEvent: NewBaseEvent(EventToolExecutionFinished, l.id),
				ToolName:  toolCall.Name,
				Duration:  toolDuration.Round(time.Millisecond),
			})
			if toolResult.IsError {
				l.emit(ToolFailedEvent{
					BaseEvent: NewBaseEvent(EventToolFailed, l.id),
					ToolName:  toolCall.Name,
					Input:     toolCall.Input,
					Error:     toolResult.Output,
					Duration:  toolDuration.Round(time.Millisecond),
				})
			} else {
				l.emit(ToolSucceededEvent{
					BaseEvent: NewBaseEvent(EventToolSucceeded, l.id),
					ToolName:  toolCall.Name,
					Input:     toolCall.Input,
					Output:    toolResult.Output,
					Duration:  toolDuration.Round(time.Millisecond),
				})
			}
			l.extensions.RunToolResult(ctx, &ToolResultContext{RunnerID: l.id, Call: toolCall, Result: &toolResult})
			outputs = append(outputs, toolResult)
		}
		if err := l.fsm.TransitionStates(ctx, LoopTransitionFinishToolExecution); err != nil {
			loopErr = fmt.Errorf("fsm finish tool execution: %w", err)
			break
		}

		// Feed the assistant's tool_use turn and the tool results back into
		// the context store; the next iteration rebuilds messages from it.
		if err := l.context.AppendAssistant(ctx, reasoningResult.Meta.AssistantMessage); err != nil {
			loopErr = fmt.Errorf("append assistant message: %w", err)
			break
		}
		if err := l.context.AppendToolResults(ctx, outputs); err != nil {
			loopErr = fmt.Errorf("append tool results: %w", err)
			break
		}
	}

	if err := l.fsm.TransitionStates(ctx, LoopTransitionReset); err != nil {
		slog.Error("fsm reset after error", "runner", l.id, "error", err)
	}
	slog.Error("loop failed", "runner", l.id, "error", loopErr, "iterations", iteration, "duration", time.Since(loopStart).Round(time.Millisecond))
	l.emit(ErrorEvent{
		BaseEvent: NewBaseEvent(EventError, l.id),
		Error:     loopErr.Error(),
		Context:   "loop",
	})
	return TurnResult{}, loopErr
}
