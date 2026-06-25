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
		NewFetcher:    NewLLMFetcherFactory(rootLLM),
		WorkingDir:    t.TempDir(),
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = engine.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected max iterations error")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("error = %q, want max iterations", err.Error())
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
