package context

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tenzing-agent/internal/provider"
)

const testContextWindow = 10_000 // yields threshold of 30_000 chars (10000 * compressAtFraction)
var testThreshold = testContextWindow * compressAtFraction

type fakeLLM struct {
	response string
	err      error
	lastReq  provider.CompletionRequest
}

func (f *fakeLLM) SendSyncMessage(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return provider.CompletionResponse{}, f.err
	}
	return provider.CompletionResponse{
		Content: []provider.ContentBlock{provider.NewTextContent(f.response)},
	}, nil
}

func (f *fakeLLM) SendStreamingMessage(context.Context, provider.CompletionRequest, chan<- provider.StreamEvent) error {
	return provider.ErrNotSupported
}

func (f *fakeLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, provider.ErrNotSupported
}

func (f *fakeLLM) CountTokens(context.Context, provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{}, provider.ErrNotSupported
}

func (f *fakeLLM) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return nil, provider.ErrNotSupported
}

func (f *fakeLLM) GetCurrentModel() string      { return "fake-model" }
func (f *fakeLLM) GetContextWindowSize() int { return 128_000 }

func makeMessages(n int, charsPer int) []provider.Message {
	msgs := make([]provider.Message, n)
	text := strings.Repeat("x", charsPer)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = provider.NewUserMessage(text)
		} else {
			msgs[i] = provider.NewAssistantMessage(text)
		}
	}
	return msgs
}

func TestEstimateSize(t *testing.T) {
	tests := []struct {
		name     string
		messages []provider.Message
		want     int
	}{
		{
			name:     "empty",
			messages: nil,
			want:     0,
		},
		{
			name:     "text blocks",
			messages: []provider.Message{provider.NewUserMessage("hello"), provider.NewAssistantMessage("world")},
			want:     10,
		},
		{
			name: "tool result block weighted",
			messages: []provider.Message{{
				Role: provider.RoleTool,
				Content: []provider.ContentBlock{
					provider.NewToolResultContent("id-1", strings.Repeat("x", 40)),
				},
			}},
			want: 10, // 40 chars / toolOutputWeight(4) = 10
		},
		{
			name: "tool use block",
			messages: []provider.Message{{
				Role: provider.RoleAssistant,
				Content: []provider.ContentBlock{
					provider.NewToolUseContent("id-1", "bash", json.RawMessage(`{"cmd":"ls"}`)),
				},
			}},
			want: 12,
		},
	}

	c := NewCompressor(nil, "unused", 0)
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
	c := NewCompressor(&fakeLLM{}, filepath.Join(t.TempDir(), MemoryFileName), testContextWindow)

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
	c := NewCompressor(&fakeLLM{}, filepath.Join(t.TempDir(), MemoryFileName), testContextWindow)

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

func TestMaybeCompressTriggered(t *testing.T) {
	dir := t.TempDir()
	llm := &fakeLLM{response: "summary of old conversation"}
	c := NewCompressor(llm, filepath.Join(dir, MemoryFileName), testContextWindow)

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

	if result[0].Role != provider.RoleUser {
		t.Fatalf("first message role = %s, want user", result[0].Role)
	}
	if !strings.Contains(result[0].Content[0].Text, "summary of old conversation") {
		t.Fatal("summary not in first message")
	}
	if result[1].Role != provider.RoleAssistant {
		t.Fatalf("second message role = %s, want assistant", result[1].Role)
	}

	// verify memory file written
	data, err := os.ReadFile(filepath.Join(dir, MemoryFileName))
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
	c := NewCompressor(llm, filepath.Join(t.TempDir(), MemoryFileName), testContextWindow)

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

func TestLoadMemoryMissing(t *testing.T) {
	c := NewCompressor(nil, filepath.Join(t.TempDir(), "nonexistent.md"), 0)

	mem, err := c.LoadMemory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mem != "" {
		t.Fatalf("expected empty, got %q", mem)
	}
}

func TestSaveAndLoadMemory(t *testing.T) {
	dir := t.TempDir()
	c := NewCompressor(nil, filepath.Join(dir, MemoryFileName), 0)

	if err := c.SaveMemory("important decisions were made"); err != nil {
		t.Fatalf("save: %v", err)
	}

	mem, err := c.LoadMemory()
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

func TestSaveMemoryOverwrites(t *testing.T) {
	dir := t.TempDir()
	c := NewCompressor(nil, filepath.Join(dir, MemoryFileName), 0)

	c.SaveMemory("first")
	c.SaveMemory("second")

	mem, _ := c.LoadMemory()
	if strings.Contains(mem, "first") {
		t.Fatal("old memory not overwritten")
	}
	if !strings.Contains(mem, "second") {
		t.Fatal("new memory not present")
	}
}

func TestSummarizeInputTruncation(t *testing.T) {
	llm := &fakeLLM{response: "truncated summary"}
	c := NewCompressor(llm, filepath.Join(t.TempDir(), MemoryFileName), testContextWindow)

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
	c := NewCompressor(nil, "", 0)
	if c.memoryFile != MemoryFileName {
		t.Fatalf("expected default %q, got %q", MemoryFileName, c.memoryFile)
	}
}

func TestCompressPreservesRecentMessages(t *testing.T) {
	dir := t.TempDir()
	llm := &fakeLLM{response: "summary"}
	c := NewCompressor(llm, filepath.Join(dir, MemoryFileName), testContextWindow)

	totalMsgs := KeepRecent + 4
	charsPerMsg := testThreshold/totalMsgs + 1
	msgs := makeMessages(totalMsgs, charsPerMsg)

	// Tag the last KeepRecent messages so we can verify they're preserved
	for i := totalMsgs - KeepRecent; i < totalMsgs; i++ {
		tag := strings.Repeat("y", charsPerMsg)
		if i%2 == 0 {
			msgs[i] = provider.NewUserMessage(tag)
		} else {
			msgs[i] = provider.NewAssistantMessage(tag)
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
