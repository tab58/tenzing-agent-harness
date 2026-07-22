package runner

import "github.com/tab58/tenzing-agent-harness/internal/core"

// FSM types and constants live in core; these aliases keep the runner API stable.
const LevelTrace = core.LevelTrace

type (
	LoopState      = core.LoopState
	LoopTransition = core.LoopTransition
	LoopFSM        = core.LoopFSM
)

const (
	LoopStateStarted               = core.LoopStateStarted
	LoopStateStopped               = core.LoopStateStopped
	LoopStateReasoningStarted      = core.LoopStateReasoningStarted
	LoopStateReasoningFinished     = core.LoopStateReasoningFinished
	LoopStateToolExecutionStarted  = core.LoopStateToolExecutionStarted
	LoopStateToolExecutionFinished = core.LoopStateToolExecutionFinished

	LoopTransitionStartReasoning      = core.LoopTransitionStartReasoning
	LoopTransitionFinishReasoning     = core.LoopTransitionFinishReasoning
	LoopTransitionStartToolExecution  = core.LoopTransitionStartToolExecution
	LoopTransitionFinishToolExecution = core.LoopTransitionFinishToolExecution
	LoopTransitionStop                = core.LoopTransitionStop
	LoopTransitionReset               = core.LoopTransitionReset
)
