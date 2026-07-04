package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"

	"github.com/tab58/llm-providers/common"
	"github.com/tab58/llm-providers/ollama"
	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/runner"
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
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: runner.LevelTrace})))

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic", "error", r, "stack", string(debug.Stack()))
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(1)
		}
	}()

	llm := ollama.NewClient(ollama.Config{
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model: common.ModelDefinition{
			Name:                 "glm-5.2",
			MaxTokens:            32_768,
			ContextWindowSize:    131_072,
			DefaultContextWindow: 32_768,
		},
	})

	mainAgent, err := agent.New(agent.AgentConfig{
		Model: llm,
	})
	if err != nil {
		slog.Error("agent init failed", "error", err)
		fmt.Fprintf(os.Stderr, "agent init failed: %v\n", err)
		os.Exit(1)
	}

	agentHarness, err := harness.New(harness.HarnessConfig{
		Cwd:      cwd,
		Agent:    mainAgent,
		RLMModel: llm,
	})
	if err != nil {
		slog.Error("harness init failed", "error", err, "stack", string(debug.Stack()))
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}

	defer agentHarness.Shutdown()

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
