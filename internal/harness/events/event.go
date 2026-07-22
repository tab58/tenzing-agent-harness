package events

import "github.com/tab58/tenzing-agent-harness/internal/core"

type (
	EventType = core.EventType
	Event     = core.Event
	BaseEvent = core.BaseEvent
)

func NewBaseEvent(eventType EventType, runnerID string) BaseEvent {
	return core.NewBaseEvent(eventType, runnerID)
}

const (
	EventSessionStarted        = core.EventSessionStarted
	EventSessionEnded          = core.EventSessionEnded
	EventTurnStarted           = core.EventTurnStarted
	EventTurnCompleted         = core.EventTurnCompleted
	EventLoopStarted           = core.EventLoopStarted
	EventLoopStopped           = core.EventLoopStopped
	EventReasoningStarted      = core.EventReasoningStarted
	EventReasoningFinished     = core.EventReasoningFinished
	EventToolExecutionStarted  = core.EventToolExecutionStarted
	EventToolExecutionFinished = core.EventToolExecutionFinished
	EventLLMResponse           = core.EventLLMResponse
	EventToolSucceeded         = core.EventToolSucceeded
	EventToolFailed            = core.EventToolFailed
	EventToolProgress          = core.EventToolProgress
	EventContextCompressing    = core.EventContextCompressing
	EventContextCompressed     = core.EventContextCompressed
	EventError                 = core.EventError
	EventSubagentStarted       = core.EventSubagentStarted
	EventSubagentStopped       = core.EventSubagentStopped
	EventTaskCreated           = core.EventTaskCreated
	EventTaskCompleted         = core.EventTaskCompleted
)
