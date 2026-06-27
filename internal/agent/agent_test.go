package agent

import (
	"context"
	"strings"
	"testing"

	"tenzing-agent/internal/provider"
)

// mockLLM implements provider.LLM for testing the agent's streaming
// and synchronous code paths.
type mockLLM struct {
	// syncResponse is returned by SendMessageWithTools.
	syncResponse provider.CompletionResponse
	syncCalled   bool

	// streamEvents are sent to the channel by SendStreamingMessage.
	streamEvents []provider.StreamEvent
	streamCalled bool
}

func (m *mockLLM) SendSyncMessage(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, nil
}

func (m *mockLLM) SendStreamingMessage(_ context.Context, _ provider.CompletionRequest, events chan<- provider.StreamEvent) error {
	m.streamCalled = true
	defer close(events)
	for _, e := range m.streamEvents {
		events <- e
	}
	return nil
}

func (m *mockLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	m.syncCalled = true
	return m.syncResponse, nil
}

func (m *mockLLM) CountTokens(_ context.Context, _ provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{InputTokens: 10}, nil
}

func (m *mockLLM) ListModels(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (m *mockLLM) GetCurrentModel() string   { return "test-model" }
func (m *mockLLM) GetContextWindowSize() int { return 128000 }

func newTestAgent(t *testing.T, llm provider.LLM) *Agent {
	t.Helper()
	ag, err := New(AgentConfig{
		Model:        llm,
		SystemPrompt: "you are a test agent",
	})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}
	return ag
}

func TestDoReasoning_StreamingDeltas(t *testing.T) {
	finalResp := provider.CompletionResponse{
		ID:         "resp-1",
		Model:      "test-model",
		StopReason: provider.StopReasonEndTurn,
		Content:    []provider.ContentBlock{provider.NewTextContent("Hello world")},
		Usage:      provider.Usage{InputTokens: 100, OutputTokens: 20},
	}

	mock := &mockLLM{
		streamEvents: []provider.StreamEvent{
			{Type: provider.StreamEventStart},
			{Type: provider.StreamEventDelta, Text: "Hello "},
			{Type: provider.StreamEventDelta, Text: "world"},
			{Type: provider.StreamEventStop, Response: &finalResp},
		},
	}

	ag := newTestAgent(t, mock)

	var collected []string
	ag.UpdateStreamCallback(func(text string) {
		collected = append(collected, text)
	})

	result, err := ag.DoReasoning(context.Background(), []string{"say hello"}, nil)
	if err != nil {
		t.Fatalf("DoReasoning error: %v", err)
	}

	// Verify deltas were forwarded through the callback.
	if len(collected) != 2 {
		t.Fatalf("expected 2 deltas, got %d: %v", len(collected), collected)
	}
	joined := strings.Join(collected, "")
	if joined != "Hello world" {
		t.Fatalf("collected deltas = %q, want %q", joined, "Hello world")
	}

	// Verify the final answer comes from the stop event's response.
	if result.FinalAnswer != "Hello world" {
		t.Fatalf("FinalAnswer = %q, want %q", result.FinalAnswer, "Hello world")
	}
	if result.Meta.Model != "test-model" {
		t.Fatalf("Meta.Model = %q, want %q", result.Meta.Model, "test-model")
	}
	if result.Meta.ResponseID != "resp-1" {
		t.Fatalf("Meta.ResponseID = %q, want %q", result.Meta.ResponseID, "resp-1")
	}

	// Verify streaming path was used, not sync.
	if !mock.streamCalled {
		t.Fatal("expected SendStreamingMessage to be called")
	}
	if mock.syncCalled {
		t.Fatal("SendMessageWithTools should not be called when streaming")
	}
}

func TestDoReasoning_NoCallbackUsesSyncPath(t *testing.T) {
	syncResp := provider.CompletionResponse{
		ID:         "resp-2",
		Model:      "test-model",
		StopReason: provider.StopReasonEndTurn,
		Content:    []provider.ContentBlock{provider.NewTextContent("sync answer")},
		Usage:      provider.Usage{InputTokens: 50, OutputTokens: 10},
	}

	mock := &mockLLM{
		syncResponse: syncResp,
	}

	ag := newTestAgent(t, mock)
	// No stream callback set.

	result, err := ag.DoReasoning(context.Background(), []string{"say hello"}, nil)
	if err != nil {
		t.Fatalf("DoReasoning error: %v", err)
	}

	if result.FinalAnswer != "sync answer" {
		t.Fatalf("FinalAnswer = %q, want %q", result.FinalAnswer, "sync answer")
	}

	// Verify sync path was used, not streaming.
	if !mock.syncCalled {
		t.Fatal("expected SendMessageWithTools to be called")
	}
	if mock.streamCalled {
		t.Fatal("SendStreamingMessage should not be called without callback")
	}
}
