package harness

import (
	"context"
	"fmt"
	"log/slog"

	"tenzing-agent/internal/tools/tooldef"
	"tenzing-agent/internal/utils"

	"github.com/looplab/fsm"
)

type LoopState string

func (s LoopState) String() string { return string(s) }

type LoopTransition string

func (t LoopTransition) String() string { return string(t) }

const (
	LoopStateStarted               LoopState = "started"
	LoopStateStopped               LoopState = "stopped"
	LoopStateReasoningStarted      LoopState = "reasoning_started"
	LoopStateReasoningFinished     LoopState = "reasoning_finished"
	LoopStateToolExecutionStarted  LoopState = "tool_execution_started"
	LoopStateToolExecutionFinished LoopState = "tool_execution_finished"

	// Moves from start -> reasoning_started.
	LoopTransitionStartReasoning LoopTransition = "start_reasoning"
	// Moves from reasoning_started -> reasoning_finished.
	LoopTransitionFinishReasoning LoopTransition = "finish_reasoning"
	// Moves from reasoning_finished -> tool_execution_started.
	LoopTransitionStartToolExecution LoopTransition = "start_tool_execution"
	// Moves from tool_execution_started -> tool_execution_finished.
	LoopTransitionFinishToolExecution LoopTransition = "finish_tool_execution"
	// Moves from reasoning_finished -> stop.
	LoopTransitionStop LoopTransition = "stop"
	// Moves from any state -> start.
	LoopTransitionReset LoopTransition = "reset"
)

var loopFSM *fsm.FSM

func init() {
	loopFSM = fsm.NewFSM(
		string(LoopStateStarted),
		fsm.Events{
			{Name: LoopTransitionStartReasoning.String(), Src: utils.Strings(LoopStateStarted), Dst: LoopStateReasoningStarted.String()},
			{Name: LoopTransitionFinishReasoning.String(), Src: utils.Strings(LoopStateReasoningStarted), Dst: LoopStateReasoningFinished.String()},
			{Name: LoopTransitionStartToolExecution.String(), Src: utils.Strings(LoopStateReasoningFinished), Dst: LoopStateToolExecutionStarted.String()},
			{Name: LoopTransitionFinishToolExecution.String(), Src: utils.Strings(LoopStateToolExecutionStarted), Dst: LoopStateToolExecutionFinished.String()},
			{Name: LoopTransitionStop.String(), Src: utils.Strings(LoopStateReasoningFinished), Dst: LoopStateStopped.String()},
			{
				Name: LoopTransitionReset.String(),
				Src: utils.Strings(
					LoopStateReasoningStarted,
					LoopStateReasoningFinished,
					LoopStateToolExecutionStarted,
					LoopStateToolExecutionFinished,
					LoopStateStopped,
				),
				Dst: LoopStateStarted.String(),
			},
		},
		fsm.Callbacks{},
	)
}

// RunTaskLoop executes a single turn: user input -> agent plan/execute loop -> agent result
func (h *Harness) RunLoop(ctx context.Context, input string) (string, error) {
	inputs := []string{input}
	var loopErr error

	transitionLoopStates(ctx, LoopTransitionReset)
	for {
		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		// Reason phase: constructs a plan based on recent input (user, tool results, etc.)
		transitionLoopStates(ctx, LoopTransitionStartReasoning)
		reminders := h.buildSystemReminders()
		reasoningResult, err := h.agent.DoReasoning(inputs, reminders)
		if err != nil {
			loopErr = fmt.Errorf("reasoning error: %w", err)
			break
		}
		transitionLoopStates(ctx, LoopTransitionFinishReasoning)

		// Decide: is tool needed?
		// exit criterion: if not tool needed, exit loop and return final answer
		if reasoningResult.ToolCall == nil {
			slog.Debug("tool not needed; returning final answer")
			finalAnswer := reasoningResult.FinalAnswer
			slog.Debug(fmt.Sprintf("final answer: %s", finalAnswer))
			transitionLoopStates(ctx, LoopTransitionStop)
			return finalAnswer, nil
		}

		if err := ctx.Err(); err != nil {
			loopErr = fmt.Errorf("loop canceled: %w", err)
			break
		}

		// Act: harness executes tool on agent's behalf
		transitionLoopStates(ctx, LoopTransitionStartToolExecution)
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
		transitionLoopStates(ctx, LoopTransitionFinishToolExecution)
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

	// if we exit the loop with an error, return it and reset loop states for next run
	transitionLoopStates(ctx, LoopTransitionReset)
	slog.Error(loopErr.Error())
	return "", loopErr
}

func (h *Harness) TransitionStates(ctx context.Context, transition LoopTransition) error {
	return transitionLoopStates(ctx, transition)
}

func transitionLoopStates(ctx context.Context, transition LoopTransition) error {
	err := loopFSM.Event(ctx, string(transition))
	if err != nil {
		return fmt.Errorf("unable to set loop state to %s: %w", transition, err)
	}
	slog.Debug(fmt.Sprintf("loop state is now %s", loopFSM.Current()))
	return nil
}
