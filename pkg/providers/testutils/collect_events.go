package testutils

import (
	"testing"

	"github.com/tab58/tenzing-agent-harness/pkg/common"
)

// collectEvents drains a stream of events, returning the text deltas and the
// final response. Fails the test on error events or a missing stop event.
func CollectEvents(t *testing.T, events <-chan common.StreamEvent) ([]string, *common.CompletionResponse) {
	t.Helper()
	var deltas []string
	var response *common.CompletionResponse
	for ev := range events {
		switch ev.Type {
		case common.StreamEventDelta:
			deltas = append(deltas, ev.Text)
		case common.StreamEventStop:
			response = ev.Response
		case common.StreamEventError:
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
	}
	if response == nil {
		t.Fatal("stream ended without a stop event")
	}
	return deltas, response
}
