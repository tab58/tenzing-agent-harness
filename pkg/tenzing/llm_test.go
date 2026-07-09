package tenzing_test

import (
	"context"
	"testing"

	"github.com/tab58/tenzing-agent-harness/pkg/tenzing"
)

// mockLLM implements the full tenzing.LLM interface using only tenzing
// aliases — proving external consumers (e.g. heimdall's test mock) never
// need to import llm-providers directly.
type mockLLM struct{}

func (m *mockLLM) ProviderName() tenzing.Provider { return tenzing.ProviderAnthropic }

func (m *mockLLM) SendSyncMessage(_ context.Context, _ tenzing.CompletionRequest) (tenzing.CompletionResponse, error) {
	return tenzing.CompletionResponse{
		Content:    []tenzing.ContentBlock{tenzing.NewTextContent("hi")},
		StopReason: tenzing.StopReasonEndTurn,
		Usage:      tenzing.Usage{},
	}, nil
}

func (m *mockLLM) SendStreamingMessage(_ context.Context, _ tenzing.CompletionRequest, events chan<- tenzing.StreamEvent) error {
	events <- tenzing.StreamEvent{Type: tenzing.StreamEventStart}
	events <- tenzing.StreamEvent{Type: tenzing.StreamEventDelta}
	events <- tenzing.StreamEvent{Type: tenzing.StreamEventStop}
	close(events)
	return nil
}

func (m *mockLLM) SendMessageWithTools(_ context.Context, _ tenzing.CompletionRequest, _ []tenzing.LLMToolDefinition) (tenzing.CompletionResponse, error) {
	return tenzing.CompletionResponse{}, nil
}

func (m *mockLLM) CountTokens(_ context.Context, _ tenzing.CompletionRequest) (tenzing.TokenCount, error) {
	return tenzing.TokenCount{}, nil
}

func (m *mockLLM) ListModels(_ context.Context) ([]tenzing.ModelInfo, error) {
	return nil, nil
}

func (m *mockLLM) GetCurrentModel() string   { return "mock" }
func (m *mockLLM) GetContextWindowSize() int { return 100000 }

// Compile-time proof the mock satisfies the aliased interface.
var _ tenzing.LLM = (*mockLLM)(nil)

func TestLLMAliasUsableInFactory(t *testing.T) {
	// The factory seam accepts an aliased LLM implementation directly.
	factory := func(_ tenzing.ModelDefinition) (tenzing.LLM, error) {
		return &mockLLM{}, nil
	}
	llm, err := factory(tenzing.Anthropic_ClaudeHaiku4_5)
	if err != nil {
		t.Fatal(err)
	}

	req := tenzing.CompletionRequest{
		Messages: []tenzing.Message{tenzing.NewUserMessage("hello")},
	}
	resp, err := llm.SendSyncMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text() != "hi" {
		t.Errorf("Text() = %q, want %q", resp.Text(), "hi")
	}
	if resp.StopReason != tenzing.StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, tenzing.StopReasonEndTurn)
	}
}

func TestStreamEventConstsAliased(t *testing.T) {
	consts := []tenzing.StreamEventType{
		tenzing.StreamEventStart,
		tenzing.StreamEventDelta,
		tenzing.StreamEventThinking,
		tenzing.StreamEventStop,
		tenzing.StreamEventError,
	}
	want := []string{"start", "delta", "thinking", "stop", "error"}
	for i, c := range consts {
		if string(c) != want[i] {
			t.Errorf("const %d = %q, want %q", i, c, want[i])
		}
	}
}

func TestClientConstructorsExported(t *testing.T) {
	// LLMFromModel with a bogus key must not panic; a client or an error
	// proves the symbol is wired to the real constructor.
	client, err := tenzing.LLMFromModel("test-key", tenzing.Anthropic_ClaudeHaiku4_5, tenzing.WithBaseURL("http://localhost:0"))
	if client == nil && err == nil {
		t.Error("LLMFromModel returned neither client nor error")
	}
	client, err = tenzing.LLMFromEnv(tenzing.Anthropic_ClaudeHaiku4_5)
	if client == nil && err == nil {
		t.Error("LLMFromEnv returned neither client nor error")
	}
}
