package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/tab58/llm-providers/common"
)

// mockLLM implements common.LLM for testing the agent's streaming
// and synchronous code paths.
type mockLLM struct {
	// syncResponse is returned by SendMessageWithTools.
	syncResponse common.CompletionResponse
	syncCalled   bool

	// streamEvents are sent to the channel by SendStreamingMessage.
	streamEvents []common.StreamEvent
	streamCalled bool
}

func (m *mockLLM) SendSyncMessage(_ context.Context, _ common.CompletionRequest) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}

func (m *mockLLM) SendStreamingMessage(_ context.Context, _ common.CompletionRequest, events chan<- common.StreamEvent) error {
	m.streamCalled = true
	defer close(events)
	for _, e := range m.streamEvents {
		events <- e
	}
	return nil
}

func (m *mockLLM) SendMessageWithTools(_ context.Context, _ common.CompletionRequest, _ []common.ToolDefinition) (common.CompletionResponse, error) {
	m.syncCalled = true
	return m.syncResponse, nil
}

func (m *mockLLM) CountTokens(_ context.Context, _ common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{InputTokens: 10}, nil
}

func (m *mockLLM) ListModels(_ context.Context) ([]common.ModelInfo, error) {
	return nil, nil
}

func (m *mockLLM) GetCurrentModel() string       { return "test-model" }
func (m *mockLLM) GetContextWindowSize() int     { return 128000 }
func (m *mockLLM) ProviderName() common.Provider { return common.ProviderOllama }

func newTestAgent(t *testing.T, llm common.LLM) *Agent {
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
	finalResp := common.CompletionResponse{
		ID:         "resp-1",
		Model:      "test-model",
		StopReason: common.StopReasonEndTurn,
		Content:    []common.ContentBlock{common.NewTextContent("Hello world")},
		Usage:      common.Usage{InputTokens: 100, OutputTokens: 20},
	}

	mock := &mockLLM{
		streamEvents: []common.StreamEvent{
			{Type: common.StreamEventStart},
			{Type: common.StreamEventDelta, Text: "Hello "},
			{Type: common.StreamEventDelta, Text: "world"},
			{Type: common.StreamEventStop, Response: &finalResp},
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
	syncResp := common.CompletionResponse{
		ID:         "resp-2",
		Model:      "test-model",
		StopReason: common.StopReasonEndTurn,
		Content:    []common.ContentBlock{common.NewTextContent("sync answer")},
		Usage:      common.Usage{InputTokens: 50, OutputTokens: 10},
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

// TestDoReasoning_ToolResultPairing verifies a tool round-trip produces a
// valid history: the user message after an assistant tool_use must carry
// tool_result blocks paired by id (the Anthropic API rejects anything else),
// and earlier inputs must not be re-appended.
func TestDoReasoning_ToolResultPairing(t *testing.T) {
	toolUseResp := common.CompletionResponse{
		ID:         "resp-1",
		Model:      "test-model",
		StopReason: common.StopReasonToolUse,
		Content: []common.ContentBlock{
			common.NewToolUseContent("tu-1", "Read", []byte(`{"path":"a.go"}`)),
			common.NewToolUseContent("tu-2", "Read", []byte(`{"path":"b.go"}`)),
		},
	}
	finalResp := common.CompletionResponse{
		ID:         "resp-2",
		Model:      "test-model",
		StopReason: common.StopReasonEndTurn,
		Content:    []common.ContentBlock{common.NewTextContent("done")},
	}

	mock := &recordingLLM{responses: []common.CompletionResponse{toolUseResp, finalResp}}
	ag := newTestAgent(t, mock)

	res, err := ag.DoReasoning(context.Background(), []string{"analyze"}, nil)
	if err != nil {
		t.Fatalf("DoReasoning 1: %v", err)
	}
	if res.ToolCall == nil || res.ToolCall.ID != "tu-1" {
		t.Fatalf("expected first tool call tu-1, got %+v", res.ToolCall)
	}

	if _, err := ag.DoReasoning(context.Background(), []string{"file contents"}, nil); err != nil {
		t.Fatalf("DoReasoning 2: %v", err)
	}

	msgs := mock.lastRequest.Messages
	if len(msgs) != 3 {
		t.Fatalf("history = %d messages, want 3 (user, assistant, tool_result message); got %+v", len(msgs), msgs)
	}
	last := msgs[2]
	if last.Role != common.RoleTool {
		t.Fatalf("last message role = %q, want tool", last.Role)
	}
	if len(last.Content) != 2 {
		t.Fatalf("tool_result blocks = %d, want 2", len(last.Content))
	}
	if last.Content[0].Type != common.ContentTypeToolResult || last.Content[0].ToolResultID != "tu-1" {
		t.Errorf("block 0 = %+v, want tool_result for tu-1", last.Content[0])
	}
	if last.Content[0].ToolOutput != "file contents" {
		t.Errorf("block 0 output = %q, want %q", last.Content[0].ToolOutput, "file contents")
	}
	if last.Content[1].Type != common.ContentTypeToolResult || last.Content[1].ToolResultID != "tu-2" {
		t.Errorf("block 1 = %+v, want placeholder tool_result for tu-2", last.Content[1])
	}
}

// recordingLLM returns canned responses in order and records the last request.
type recordingLLM struct {
	responses   []common.CompletionResponse
	calls       int
	lastRequest common.CompletionRequest
}

func (m *recordingLLM) SendSyncMessage(_ context.Context, _ common.CompletionRequest) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}

func (m *recordingLLM) SendStreamingMessage(_ context.Context, _ common.CompletionRequest, events chan<- common.StreamEvent) error {
	close(events)
	return nil
}

func (m *recordingLLM) SendMessageWithTools(_ context.Context, req common.CompletionRequest, _ []common.ToolDefinition) (common.CompletionResponse, error) {
	m.lastRequest = req
	resp := m.responses[min(m.calls, len(m.responses)-1)]
	m.calls++
	return resp, nil
}

func (m *recordingLLM) CountTokens(_ context.Context, _ common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{InputTokens: 10}, nil
}

func (m *recordingLLM) ListModels(_ context.Context) ([]common.ModelInfo, error) { return nil, nil }
func (m *recordingLLM) GetCurrentModel() string                                  { return "test-model" }
func (m *recordingLLM) GetContextWindowSize() int                                { return 128000 }
func (m *recordingLLM) ProviderName() common.Provider                            { return common.ProviderOllama }
