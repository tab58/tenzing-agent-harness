package compressor

import (
	"context"
	"strings"
	"testing"
)

// Manual compaction bypasses the size threshold entirely.
func TestCompressBypassesThreshold(t *testing.T) {
	llm := &fakeLLM{response: "tiny summary"}
	c := newTestCompressor(t, llm, 128_000)

	msgs := makeMessages(10, 10) // far below any threshold
	compressed, summary, did, err := c.Compress(context.Background(), msgs, "")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if !did {
		t.Fatal("manual compress should run despite being below threshold")
	}
	if len(compressed) >= len(msgs) {
		t.Errorf("compressed = %d messages, want fewer than %d", len(compressed), len(msgs))
	}
	if !strings.Contains(summary, "tiny summary") {
		t.Errorf("summary = %q", summary)
	}
}

func TestCompressInstructionsReachSummarizer(t *testing.T) {
	llm := &fakeLLM{response: "s"}
	c := newTestCompressor(t, llm, 128_000)

	_, _, _, err := c.Compress(context.Background(), makeMessages(10, 10), "focus on the auth work")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	prompt := llm.lastReq.Messages[0].Content[0].Text
	if !strings.Contains(prompt, "focus on the auth work") {
		t.Error("instructions did not reach the summarizer prompt")
	}
}

func TestCompressTooFewMessagesIsNoop(t *testing.T) {
	c := newTestCompressor(t, &fakeLLM{response: "s"}, 128_000)
	msgs := makeMessages(4, 10) // at or under keepRecent
	_, _, did, err := c.Compress(context.Background(), msgs, "")
	if err != nil || did {
		t.Fatalf("Compress on short history = (did=%v, err=%v), want noop", did, err)
	}
}

func TestSetKeepRecentHonored(t *testing.T) {
	llm := &fakeLLM{response: "s"}
	c := newTestCompressor(t, llm, 128_000)
	c.SetKeepRecent(2)

	msgs := makeMessages(10, 10)
	compressed, _, did, err := c.Compress(context.Background(), msgs, "")
	if err != nil || !did {
		t.Fatalf("Compress: did=%v err=%v", did, err)
	}
	// summary msg (+ optional todo) + ack + 2 recent = 4 (no todo provider)
	if len(compressed) != 4 {
		t.Errorf("compressed = %d messages, want 4 (keepRecent=2)", len(compressed))
	}
}
