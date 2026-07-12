package context

import (
	"context"
	"strings"
	"testing"

	"github.com/tab58/llm-providers/common"
)

type stubLLM struct{}

func (s *stubLLM) SendSyncMessage(context.Context, common.CompletionRequest) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}
func (s *stubLLM) SendStreamingMessage(context.Context, common.CompletionRequest, chan<- common.StreamEvent) error {
	return nil
}
func (s *stubLLM) SendMessageWithTools(context.Context, common.CompletionRequest, []common.ToolDefinition) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, nil
}
func (s *stubLLM) CountTokens(context.Context, common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{}, nil
}
func (s *stubLLM) ListModels(context.Context) ([]common.ModelInfo, error) { return nil, nil }
func (s *stubLLM) GetCurrentModel() string                                { return "stub" }
func (s *stubLLM) GetContextWindowSize() int                              { return 4096 }
func (s *stubLLM) ProviderName() common.Provider                          { return common.ProviderOllama }

func TestNewContextInjectsInitialMemory(t *testing.T) {
	ctx, err := NewContext(ContextConfig{LLM: &stubLLM{}, InitialMemory: "prior work summary"})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	if ctx.Len() != 2 {
		t.Fatalf("len = %d, want 2 (summary + ack)", ctx.Len())
	}
	first := ctx.Messages()[0]
	if first.Role != common.RoleUser || !strings.Contains(first.Content[0].Text, "prior work summary") {
		t.Fatalf("first message = %+v, want user message containing memory", first)
	}
}

func TestNewContextWithoutInitialMemoryStartsEmpty(t *testing.T) {
	ctx, err := NewContext(ContextConfig{LLM: &stubLLM{}})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	if ctx.Len() != 0 {
		t.Fatalf("len = %d, want 0", ctx.Len())
	}
}

// Regression: compression info reached the runner without the summary text,
// so the harness persisted empty memory files (52-byte headers). The summary
// must flow back from AppendMessages.
func TestAppendMessagesReturnsCompressionSummary(t *testing.T) {
	ctx, err := NewContext(ContextConfig{LLM: &compressingLLM{}})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}

	// Fill history past the threshold (stub window 4096 tokens -> threshold
	// 12288 chars), ending with an assistant message to trigger the check.
	big := strings.Repeat("x", 2000)
	var msgs []common.Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs, common.NewUserMessage(big), common.NewAssistantMessage(big))
	}
	compressed, summary, err := ctx.AppendMessages(context.Background(), msgs...)
	if err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}
	if !compressed {
		t.Fatal("expected compression to trigger")
	}
	if !strings.Contains(summary, "the summary text") {
		t.Fatalf("summary = %q, want the compressor's summary text", summary)
	}
}

// compressingLLM returns a fixed summary for the compression call.
type compressingLLM struct{ stubLLM }

func (c *compressingLLM) SendSyncMessage(context.Context, common.CompletionRequest) (common.CompletionResponse, error) {
	return common.CompletionResponse{
		Content: []common.ContentBlock{common.NewTextContent("the summary text")},
	}, nil
}
