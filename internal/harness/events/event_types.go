package events

import "github.com/tab58/tenzing-agent-harness/internal/core"

type (
	SessionStartedEvent        = core.SessionStartedEvent
	SessionEndedEvent          = core.SessionEndedEvent
	TurnStartedEvent           = core.TurnStartedEvent
	TurnCompletedEvent         = core.TurnCompletedEvent
	LoopStartedEvent           = core.LoopStartedEvent
	LoopStoppedEvent           = core.LoopStoppedEvent
	ReasoningStartedEvent      = core.ReasoningStartedEvent
	ReasoningFinishedEvent     = core.ReasoningFinishedEvent
	LLMResponseEvent           = core.LLMResponseEvent
	ToolExecutionStartedEvent  = core.ToolExecutionStartedEvent
	ToolExecutionFinishedEvent = core.ToolExecutionFinishedEvent
	ToolSucceededEvent         = core.ToolSucceededEvent
	ToolFailedEvent            = core.ToolFailedEvent
	ToolProgressEvent          = core.ToolProgressEvent
	ContextCompressingEvent    = core.ContextCompressingEvent
	ContextCompressedEvent     = core.ContextCompressedEvent
	ErrorEvent                 = core.ErrorEvent
	SubagentStartedEvent       = core.SubagentStartedEvent
	SubagentStoppedEvent       = core.SubagentStoppedEvent
	TaskCreatedEvent           = core.TaskCreatedEvent
	TaskCompletedEvent         = core.TaskCompletedEvent
)
