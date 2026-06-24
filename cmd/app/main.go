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
	agentctx "tenzing-agent/internal/agent/context"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/tools"
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

	taskGraph := agentctx.NewTaskGraph(cwd)

	skillsRegistry := skills.NewRegistry(
		"~/.claude/skills",
	)

	toolDefs, err := tools.GetDefaultToolDefs(skillsRegistry, taskGraph, cwd, &tools.RLMConfig{
		RootModel: llm,
		MaxDepth:  1,
	})
	if err != nil {
		slog.Error("tool init failed", "error", err)
		fmt.Fprintf(os.Stderr, "tool init failed: %v\n", err)
		os.Exit(1)
	}

	toolRegistry := tools.NewRegistry(cwd, toolDefs...)

	hooks := harness.Hooks{}

	systemPrompt := prompts.DefaultSystemPrompt(cwd) + "\n\n" + prompts.RLMGuidance()

	mainAgent := agent.NewWithCompressor(agent.AgentConfig{
		Model:           llm,
		ToolDefinitions: toolRegistry.ProviderDefinitions(),
	}, cwd)

	agentHarness, err := harness.New(harness.HarnessConfig{
		MainRunner: harness.AgentRunnerConfig{
			Agent:          mainAgent,
			ToolRegistry:   toolRegistry,
			Hooks:          hooks,
			SystemPrompt:   systemPrompt,
			BuildReminders: harness.DefaultReminderBuilder(cwd, taskGraph),
		},
	})
	if err != nil {
		slog.Error("harness init failed", "error", err, "stack", string(debug.Stack()))
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}

	slog.Info("session started", "model", llm.GetCurrentModel(), "cwd", cwd, "tools", len(toolRegistry.Definitions()))

	fmt.Println("tenzing agent harness")
	fmt.Printf("  model  %s\n", llm.GetCurrentModel())
	fmt.Printf("  cwd    %s\n", cwd)
	fmt.Printf("  tools  %d registered\n", len(toolRegistry.Definitions()))
	fmt.Println()

	err = agentHarness.RunSession(ctx, os.Stdin, os.Stdout)
	if err != nil {
		slog.Error("session ended with error", "error", err)
		fmt.Fprintf(os.Stderr, "session error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("session ended")
}
