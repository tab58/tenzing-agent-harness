package nexus

import "tenzing-agent/internal/harness/events"

// Nexus event types published on the harness event bus. Defined here (not
// in the events package) so the harness stays ignorant of nexus.
const (
	EventChannelError  events.EventType = "nexus.channel_error"
	EventChannelStatus events.EventType = "nexus.channel_status"
	EventTrigger       events.EventType = "nexus.trigger"
)

const runnerID = "nexus"

// ChannelErrorEvent fires for each buffered line matching a channel's
// error pattern.
type ChannelErrorEvent struct {
	events.BaseEvent
	Channel string `json:"channel"`
	Text    string `json:"text"`
	Seq     uint64 `json:"seq"`
}

// ChannelStatusEvent fires on source lifecycle changes
// (running/restarting/stopped).
type ChannelStatusEvent struct {
	events.BaseEvent
	Channel string `json:"channel"`
	State   string `json:"state"`
}

// TriggerEvent fires when an error-triggered agent turn actually starts.
type TriggerEvent struct {
	events.BaseEvent
	Channels []string `json:"channels"`
}
