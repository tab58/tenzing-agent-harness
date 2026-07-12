package compressor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tab58/llm-providers/common"
)

const testContextWindow = 10_000 // yields threshold of 30_000 chars (10000 * compressAtFraction)
var testThreshold = testContextWindow * compressAtFraction

type fakeLLM struct {
	response string
	err      error
	lastReq  common.CompletionRequest
}

func (f *fakeLLM) SendSyncMessage(_ context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return common.CompletionResponse{}, f.err
	}
	return common.CompletionResponse{
		Content: []common.ContentBlock{common.NewTextContent(f.response)},
	}, nil
}

func (f *fakeLLM) SendStreamingMessage(context.Context, common.CompletionRequest, chan<- common.StreamEvent) error {
	return common.ErrNotSupported
}

func (f *fakeLLM) SendMessageWithTools(_ context.Context, _ common.CompletionRequest, _ []common.ToolDefinition) (common.CompletionResponse, error) {
	return common.CompletionResponse{}, common.ErrNotSupported
}

func (f *fakeLLM) CountTokens(context.Context, common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{}, common.ErrNotSupported
}

func (f *fakeLLM) ListModels(context.Context) ([]common.ModelInfo, error) {
	return nil, common.ErrNotSupported
}

func (f *fakeLLM) GetCurrentModel() string       { return "fake-model" }
func (f *fakeLLM) GetContextWindowSize() int     { return 128_000 }
func (f *fakeLLM) ProviderName() common.Provider { return common.ProviderOllama }

func newTestCompressor(t *testing.T, llm common.LLM, contextWindow int) *Compressor {
	t.Helper()
	c := NewCompressor(llm, contextWindow)
	c.memoryFile = filepath.Join(t.TempDir(), MemoryFileName)
	return c
}

func makeMessages(n int, charsPer int) []common.Message {
	msgs := make([]common.Message, n)
	text := strings.Repeat("x", charsPer)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = common.NewUserMessage(text)
		} else {
			msgs[i] = common.NewAssistantMessage(text)
		}
	}
	return msgs
}

func TestEstimateSize(t *testing.T) {
	tests := []struct {
		name     string
		messages []common.Message
		want     int
	}{
		{
			name:     "empty",
			messages: nil,
			want:     0,
		},
		{
			name:     "text blocks",
			messages: []common.Message{common.NewUserMessage("hello"), common.NewAssistantMessage("world")},
			want:     10,
		},
		{
			name: "tool result block weighted",
			messages: []common.Message{{
				Role: common.RoleTool,
				Content: []common.ContentBlock{
					common.NewToolResultContent("id-1", "tool", strings.Repeat("x", 40)),
				},
			}},
			want: 10, // 40 chars / toolOutputWeight(4) = 10
		},
		{
			name: "tool use block",
			messages: []common.Message{{
				Role: common.RoleAssistant,
				Content: []common.ContentBlock{
					common.NewToolUseContent("id-1", "bash", json.RawMessage(`{"cmd":"ls"}`)),
				},
			}},
			want: 12,
		},
	}

	c := NewCompressor(nil, 0)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.EstimateSize(tt.messages)
			if got != tt.want {
				t.Errorf("EstimateSize = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMaybeCompressBelowThreshold(t *testing.T) {
	c := newTestCompressor(t, &fakeLLM{}, testContextWindow)

	msgs := makeMessages(10, 100)
	result, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did {
		t.Fatal("should not compress below threshold")
	}
	if len(result) != len(msgs) {
		t.Fatalf("messages changed: got %d want %d", len(result), len(msgs))
	}
}

func TestMaybeCompressTooFewMessages(t *testing.T) {
	c := newTestCompressor(t, &fakeLLM{}, testContextWindow)

	msgs := makeMessages(KeepRecent, testThreshold/KeepRecent+1)
	result, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did {
		t.Fatal("should not compress when <= KeepRecent messages")
	}
	if len(result) != len(msgs) {
		t.Fatalf("messages changed: got %d want %d", len(result), len(msgs))
	}
}

// Summarization is a plumbing call: it must explicitly disable model
// reasoning so thinking-by-default models don't burn tokens on it.
func TestSummarizeDisablesThinking(t *testing.T) {
	llm := &fakeLLM{response: "summary"}
	c := newTestCompressor(t, llm, testContextWindow)

	totalMsgs := KeepRecent + 4
	msgs := makeMessages(totalMsgs, testThreshold/totalMsgs+1)
	if _, did, err := c.MaybeCompress(context.Background(), msgs); err != nil || !did {
		t.Fatalf("MaybeCompress did=%v err=%v, want compression", did, err)
	}
	if llm.lastReq.Think == nil || *llm.lastReq.Think {
		t.Errorf("summarize request Think = %v, want explicit false", llm.lastReq.Think)
	}
}

func TestMaybeCompressTriggered(t *testing.T) {
	llm := &fakeLLM{response: "summary of old conversation"}
	c := newTestCompressor(t, llm, testContextWindow)

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	result, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatal("expected compression to trigger")
	}

	// 2 (summary + ack) + KeepRecent
	expectedLen := 2 + KeepRecent
	if len(result) != expectedLen {
		t.Fatalf("compressed len = %d, want %d", len(result), expectedLen)
	}

	if result[0].Role != common.RoleUser {
		t.Fatalf("first message role = %s, want user", result[0].Role)
	}
	if !strings.Contains(result[0].Content[0].Text, "summary of old conversation") {
		t.Fatal("summary not in first message")
	}
	if result[1].Role != common.RoleAssistant {
		t.Fatalf("second message role = %s, want assistant", result[1].Role)
	}

	// verify memory file written
	data, err := os.ReadFile(c.memoryFile)
	if err != nil {
		t.Fatalf("memory file not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "summary of old conversation") {
		t.Fatal("memory file missing summary")
	}
	if !strings.Contains(content, "# Agent Memory") {
		t.Fatal("memory file missing header")
	}
}

func TestMaybeCompressLLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("api down")}
	c := newTestCompressor(t, llm, testContextWindow)

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	result, did, err := c.MaybeCompress(context.Background(), msgs)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if did {
		t.Fatal("should not report compressed on error")
	}
	if len(result) != len(msgs) {
		t.Fatal("original messages should be returned on error")
	}
}

func TestLoadFromMemoryFileMissing(t *testing.T) {
	c := NewCompressor(nil, 0)
	c.memoryFile = filepath.Join(t.TempDir(), "nonexistent.md")

	mem, err := c.LoadFromMemoryFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mem != "" {
		t.Fatalf("expected empty, got %q", mem)
	}
}

func TestSaveAndLoadMemoryFile(t *testing.T) {
	c := newTestCompressor(t, nil, 0)

	if err := c.SaveToMemoryFile("important decisions were made"); err != nil {
		t.Fatalf("save: %v", err)
	}

	mem, err := c.LoadFromMemoryFile()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(mem, "important decisions were made") {
		t.Fatal("loaded memory missing content")
	}
	if !strings.Contains(mem, "# Agent Memory") {
		t.Fatal("loaded memory missing header")
	}
	if !strings.Contains(mem, "Updated:") {
		t.Fatal("loaded memory missing timestamp")
	}
}

func TestSaveMemoryFileOverwrites(t *testing.T) {
	c := newTestCompressor(t, nil, 0)

	c.SaveToMemoryFile("first")
	c.SaveToMemoryFile("second")

	mem, _ := c.LoadFromMemoryFile()
	if strings.Contains(mem, "first") {
		t.Fatal("old memory not overwritten")
	}
	if !strings.Contains(mem, "second") {
		t.Fatal("new memory not present")
	}
}

func TestCompressTodoInjection(t *testing.T) {
	llm := &fakeLLM{response: "conversation summary here"}
	c := newTestCompressor(t, llm, testContextWindow)
	c.SetTodoProvider(func() string {
		return "<system-reminder>\nCurrent plan:\n[abc12345] [pending] do the thing\n</system-reminder>"
	})

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	compressed, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected compression to trigger")
	}

	// expect: summary + todo + ack + KeepRecent recent messages
	expectedLen := 3 + KeepRecent
	if len(compressed) != expectedLen {
		t.Fatalf("compressed len = %d, want %d", len(compressed), expectedLen)
	}

	// find the todo injection message
	foundTodo := false
	for _, msg := range compressed {
		for _, block := range msg.Content {
			if strings.Contains(block.Text, "Current plan:") && strings.Contains(block.Text, "abc12345") {
				foundTodo = true
			}
		}
	}
	if !foundTodo {
		t.Error("todo state not found in compressed messages")
	}
}

func TestCompressTodoInjectionSkippedWhenEmpty(t *testing.T) {
	llm := &fakeLLM{response: "summary"}
	c := newTestCompressor(t, llm, testContextWindow)
	c.SetTodoProvider(func() string { return "" })

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	compressed, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected compression to trigger")
	}

	// no todo injection: summary + ack + KeepRecent
	expectedLen := 2 + KeepRecent
	if len(compressed) != expectedLen {
		t.Fatalf("compressed len = %d, want %d (empty todo should not inject)", len(compressed), expectedLen)
	}
}

func TestSummarizeInputTruncation(t *testing.T) {
	llm := &fakeLLM{response: "truncated summary"}
	c := newTestCompressor(t, llm, testContextWindow)

	totalMsgs := KeepRecent + 2
	charsPerMsg := maxSummarizeInput
	msgs := makeMessages(totalMsgs, charsPerMsg)

	_, did, err := c.MaybeCompress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatal("expected compression")
	}

	// LLM should have received truncated input
	inputText := llm.lastReq.Messages[0].Content[0].Text
	if len(inputText) > maxSummarizeInput+200 {
		t.Fatalf("input not truncated: len=%d", len(inputText))
	}
}

func TestNewCompressorDefaultMemoryFile(t *testing.T) {
	c := NewCompressor(nil, 0)
	if c.memoryFile != MemoryFileName {
		t.Fatalf("expected default %q, got %q", MemoryFileName, c.memoryFile)
	}
}

func TestCompressPreservesRecentMessages(t *testing.T) {
	llm := &fakeLLM{response: "summary"}
	c := newTestCompressor(t, llm, testContextWindow)

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	// Tag the last KeepRecent messages so we can verify they're preserved
	for i := totalMsgs - KeepRecent; i < totalMsgs; i++ {
		tag := strings.Repeat("y", charsPerMsg)
		if i%2 == 0 {
			msgs[i] = common.NewUserMessage(tag)
		} else {
			msgs[i] = common.NewAssistantMessage(tag)
		}
	}

	result, _, _ := c.MaybeCompress(context.Background(), msgs)

	// Skip summary pair (indices 0,1), check rest match recent
	for i := 2; i < len(result); i++ {
		got := result[i].Content[0].Text
		want := msgs[totalMsgs-KeepRecent+(i-2)].Content[0].Text
		if got != want {
			t.Fatalf("recent message %d not preserved", i-2)
		}
	}
}
