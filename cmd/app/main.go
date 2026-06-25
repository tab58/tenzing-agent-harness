package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"

	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/provider"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	logFile, err := os.OpenFile(filepath.Join(cwd, ".tenzing-agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: harness.LevelTrace})))

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic", "error", r, "stack", string(debug.Stack()))
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(1)
		}
	}()

	llm := provider.NewOllamaClient(provider.OllamaConfig{
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model:  "glm-5.2",
	})

	// register hooks
	hooks := harness.Hooks{}

	mainAgent := agent.NewWithCompressor(agent.AgentConfig{
		Model: llm,
	}, cwd)

	agentHarness, err := harness.New(harness.HarnessConfig{
		Cwd:              cwd,
		Agent:            mainAgent,
		Hooks:            hooks,
		MainSystemPrompt: prompts.DefaultSystemPrompt(cwd) + "\n\n" + prompts.RLMGuidance(),
	})
	if err != nil {
		slog.Error("harness init failed", "error", err, "stack", string(debug.Stack()))
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}

	slog.Info("session started", "model", llm.GetCurrentModel(), "cwd", cwd, "tools", len(agentHarness.ToolDefinitions()))

	fmt.Println("tenzing agent harness")
	fmt.Printf("  model  %s\n", llm.GetCurrentModel())
	fmt.Printf("  cwd    %s\n", cwd)
	fmt.Printf("  tools  %d registered\n", len(agentHarness.ToolDefinitions()))
	fmt.Println()

	err = agentHarness.RunSession(ctx, os.Stdin, os.Stdout)
	if err != nil {
		slog.Error("session ended with error", "error", err)
		fmt.Fprintf(os.Stderr, "session error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("session ended")
}
