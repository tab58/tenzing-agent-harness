package events

// Hooks holds optional typed callback functions, one per event type.
// Set only the hooks you care about; nil hooks are silently skipped.
type Hooks struct {
	OnSessionStarted        func(SessionStartedEvent)
	OnSessionEnded          func(SessionEndedEvent)
	OnTurnStarted           func(TurnStartedEvent)
	OnTurnCompleted         func(TurnCompletedEvent)
	OnLoopStarted           func(LoopStartedEvent)
	OnLoopStopped           func(LoopStoppedEvent)
	OnReasoningStarted      func(ReasoningStartedEvent)
	OnReasoningFinished     func(ReasoningFinishedEvent)
	OnToolExecutionStarted  func(ToolExecutionStartedEvent)
	OnToolExecutionFinished func(ToolExecutionFinishedEvent)
	OnLLMResponse           func(LLMResponseEvent)
	OnToolSucceeded         func(ToolSucceededEvent)
	OnToolFailed            func(ToolFailedEvent)
	OnToolProgress          func(ToolProgressEvent)
	OnContextCompressing    func(ContextCompressingEvent)
	OnContextCompressed     func(ContextCompressedEvent)
	OnError                 func(ErrorEvent)
	OnSubagentStarted       func(SubagentStartedEvent)
	OnSubagentStopped       func(SubagentStoppedEvent)
	OnTaskCreated           func(TaskCreatedEvent)
	OnTaskCompleted         func(TaskCompletedEvent)
}

// HooksAdapter subscribes to an EventBus and dispatches each received event
// to the matching typed hook function.
type HooksAdapter struct {
	ch   <-chan Event
	bus  *EventBus
	done chan struct{}
}

// NewHooksAdapter subscribes to bus with a buffer of 64 and starts a dispatch
// goroutine. Call Stop to unsubscribe and wait for the goroutine to exit.
func NewHooksAdapter(bus *EventBus, hooks Hooks) *HooksAdapter {
	ch := bus.Subscribe(64)
	a := &HooksAdapter{ch: ch, bus: bus, done: make(chan struct{})}
	go a.run(hooks)
	return a
}

// Stop unsubscribes from the bus and waits for the dispatch goroutine to exit.
func (a *HooksAdapter) Stop() {
	a.bus.Unsubscribe(a.ch)
	<-a.done
}

func (a *HooksAdapter) run(hooks Hooks) {
	defer close(a.done)
	for ev := range a.ch {
		dispatch(ev, hooks)
	}
}

func dispatch(ev Event, h Hooks) {
	switch e := ev.(type) {
	case SessionStartedEvent:
		if h.OnSessionStarted != nil {
			h.OnSessionStarted(e)
		}
	case SessionEndedEvent:
		if h.OnSessionEnded != nil {
			h.OnSessionEnded(e)
		}
	case TurnStartedEvent:
		if h.OnTurnStarted != nil {
			h.OnTurnStarted(e)
		}
	case TurnCompletedEvent:
		if h.OnTurnCompleted != nil {
			h.OnTurnCompleted(e)
		}
	case LoopStartedEvent:
		if h.OnLoopStarted != nil {
			h.OnLoopStarted(e)
		}
	case LoopStoppedEvent:
		if h.OnLoopStopped != nil {
			h.OnLoopStopped(e)
		}
	case ReasoningStartedEvent:
		if h.OnReasoningStarted != nil {
			h.OnReasoningStarted(e)
		}
	case ReasoningFinishedEvent:
		if h.OnReasoningFinished != nil {
			h.OnReasoningFinished(e)
		}
	case ToolExecutionStartedEvent:
		if h.OnToolExecutionStarted != nil {
			h.OnToolExecutionStarted(e)
		}
	case ToolExecutionFinishedEvent:
		if h.OnToolExecutionFinished != nil {
			h.OnToolExecutionFinished(e)
		}
	case LLMResponseEvent:
		if h.OnLLMResponse != nil {
			h.OnLLMResponse(e)
		}
	case ToolSucceededEvent:
		if h.OnToolSucceeded != nil {
			h.OnToolSucceeded(e)
		}
	case ToolFailedEvent:
		if h.OnToolFailed != nil {
			h.OnToolFailed(e)
		}
	case ToolProgressEvent:
		if h.OnToolProgress != nil {
			h.OnToolProgress(e)
		}
	case ContextCompressingEvent:
		if h.OnContextCompressing != nil {
			h.OnContextCompressing(e)
		}
	case ContextCompressedEvent:
		if h.OnContextCompressed != nil {
			h.OnContextCompressed(e)
		}
	case ErrorEvent:
		if h.OnError != nil {
			h.OnError(e)
		}
	case SubagentStartedEvent:
		if h.OnSubagentStarted != nil {
			h.OnSubagentStarted(e)
		}
	case SubagentStoppedEvent:
		if h.OnSubagentStopped != nil {
			h.OnSubagentStopped(e)
		}
	case TaskCreatedEvent:
		if h.OnTaskCreated != nil {
			h.OnTaskCreated(e)
		}
	case TaskCompletedEvent:
		if h.OnTaskCompleted != nil {
			h.OnTaskCompleted(e)
		}
	}
}
