package harness

import (
	"context"
	"fmt"
	"log/slog"

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

type LoopFSM struct {
	*fsm.FSM
}

func (f *LoopFSM) TransitionStates(ctx context.Context, transition LoopTransition) error {
	err := f.Event(ctx, string(transition))
	if err != nil {
		return fmt.Errorf("unable to set loop state to %s: %w", transition, err)
	}
	slog.Debug(fmt.Sprintf("loop state is now %s", f.Current()))
	return nil
}

func createNewLoopFSM() *LoopFSM {
	f := fsm.NewFSM(
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
					LoopStateStarted,
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
	return &LoopFSM{FSM: f}
}
