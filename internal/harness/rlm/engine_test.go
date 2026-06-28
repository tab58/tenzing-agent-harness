package rlm

import (
	"context"
	"strings"
	"testing"

	"tenzing-agent/internal/provider"
)

// scriptedLLM returns canned responses in sequence
type scriptedLLM struct {
	responses    []string
	idx          int
	callCount    int
	lastMessages []provider.Message
}

func (s *scriptedLLM) SendSyncMessage(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	s.callCount++
	s.lastMessages = req.Messages
	if s.idx >= len(s.responses) {
		return provider.CompletionResponse{
			Content: []provider.ContentBlock{provider.NewTextContent("FINAL(\"exhausted\")")},
		}, nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return provider.CompletionResponse{
		Content: []provider.ContentBlock{provider.NewTextContent(resp)},
	}, nil
}

func (s *scriptedLLM) SendStreamingMessage(context.Context, provider.CompletionRequest, chan<- provider.StreamEvent) error {
	return provider.ErrNotSupported
}

func (s *scriptedLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, provider.ErrNotSupported
}

func (s *scriptedLLM) CountTokens(context.Context, provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{}, provider.ErrNotSupported
}

func (s *scriptedLLM) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return nil, provider.ErrNotSupported
}

func (s *scriptedLLM) GetCurrentModel() string      { return "scripted-model" }
func (s *scriptedLLM) GetContextWindowSize() int { return 128_000 }

func TestEngineSimpleFinal(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"The answer is clear.\nFINAL(\"42\")",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(rootLLM),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	answer, err := engine.Run(context.Background(), "what is the meaning of life?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "42" {
		t.Fatalf("answer = %q, want %q", answer, "42")
	}
}

func TestEngineCodeExecution(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"Let me check the prompt.\n```repl\nprint(len(prompt))\n```",
		"Got it.\nFINAL(\"done\")",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(rootLLM),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	answer, err := engine.Run(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "done" {
		t.Fatalf("answer = %q, want %q", answer, "done")
	}
	// Verify LLM saw the REPL output (prompt length = 11)
	found := false
	for _, msg := range rootLLM.lastMessages {
		for _, block := range msg.Content {
			if strings.Contains(block.Text, "11") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("REPL output (prompt length) not fed back to LLM")
	}
}

func TestEngineSubLMFromPython(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"```repl\nresult = sub_lm(\"summarize this\")\nFINAL(result)\n```",
	}}
	querier := &fakeQuerier{response: "a concise summary"}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(rootLLM),
		Querier:    querier,
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	answer, err := engine.Run(context.Background(), "some long text")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "a concise summary" {
		t.Fatalf("answer = %q, want %q", answer, "a concise summary")
	}
}

func TestEngineMaxIterations(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"```repl\nprint(\"still going\")\n```",
		"```repl\nprint(\"still going\")\n```",
		"```repl\nprint(\"still going\")\n```",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher:        NewLLMFetcherFactory(rootLLM),
		WorkingDir:        t.TempDir(),
		DefaultIterations: 3,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	result, err := engine.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[RLM partial result") {
		t.Fatalf("expected partial result marker, got %q", result)
	}
}

func TestEngineDefaultIterations(t *testing.T) {
	e, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(&scriptedLLM{}),
		Querier:    &fakeQuerier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.defaultIterations != 30 {
		t.Errorf("defaultIterations = %d, want 30", e.defaultIterations)
	}
	if e.maxIterations != 200 {
		t.Errorf("maxIterations = %d, want 200", e.maxIterations)
	}
}

func TestEngineResolveLimit(t *testing.T) {
	e := &Engine{defaultIterations: 30, maxIterations: 200}

	tests := []struct {
		name     string
		override int
		want     int
	}{
		{"no override uses default", 0, 30},
		{"override within ceiling", 100, 100},
		{"override above ceiling capped", 500, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.resolveLimit(tt.override)
			if got != tt.want {
				t.Errorf("resolveLimit(%d) = %d, want %d", tt.override, got, tt.want)
			}
		})
	}
}

func TestEngineNudgeOnNoCode(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"I think the answer is blah blah blah",
		"FINAL(\"the real answer\")",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(rootLLM),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	answer, err := engine.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "the real answer" {
		t.Fatalf("answer = %q, want %q", answer, "the real answer")
	}
	if rootLLM.callCount < 2 {
		t.Fatal("expected at least 2 LLM calls (original + after nudge)")
	}
}

func TestEnginePromptNotInContext(t *testing.T) {
	skipIfNoPython(t)

	bigPrompt := strings.Repeat("x", 10000)
	rootLLM := &scriptedLLM{responses: []string{
		"FINAL(\"done\")",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewLLMFetcherFactory(rootLLM),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = engine.Run(context.Background(), bigPrompt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, msg := range rootLLM.lastMessages {
		for _, block := range msg.Content {
			if strings.Contains(block.Text, bigPrompt) {
				t.Fatal("full prompt found in LLM context — should only be in REPL")
			}
		}
	}
}

func TestEngineSimpleFetcher(t *testing.T) {
	skipIfNoPython(t)

	rootLLM := &scriptedLLM{responses: []string{
		"The answer is clear.\nFINAL(\"42\")",
	}}

	engine, err := NewEngine(EngineConfig{
		NewFetcher: NewSimpleFetcherFactory(rootLLM),
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	answer, err := engine.Run(context.Background(), "what is the meaning of life?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "42" {
		t.Fatalf("answer = %q, want %q", answer, "42")
	}
}

func TestComputeChunkInfo(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single block", "hello world", "[11]"},
		{"two paragraphs", "hello\n\nworld", "[5, 5]"},
		{"three paragraphs", "aaa\n\nbb\n\nccccc", "[3, 2, 5]"},
		{"empty", "", "[0]"},
		{"trailing double newline", "abc\n\n", "[5]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeChunkInfo(tt.input)
			if got != tt.want {
				t.Errorf("computeChunkInfo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
