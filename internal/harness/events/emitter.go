package events

// Emitter is the interface for publishing events.
// Implementations may fan out to subscribers, write to a log, etc.
type Emitter interface {
	Emit(event Event)
}
