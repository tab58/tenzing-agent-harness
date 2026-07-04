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
	"tenzing-agent/internal/harness/runner"

	"github.com/tab58/huma-http-server/config"
	"github.com/tab58/llm-providers/common"
	"github.com/tab58/llm-providers/ollama"
)

var cfg *Config

func init() {
	cfg = &Config{}
	if err := config.Load(cfg); err != nil {
		slog.Error("failed to load config", "error", err)
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
}

type Config struct {
	APIKeyOllama string `mapstructure:"OLLAMA_API_KEY" sensitive:"true"`
}

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

	agentHarness, err := createAgent(cwd)
	if err != nil {
		slog.Error("failed to create agent", "error", err)
		fmt.Fprintf(os.Stderr, "failed to create agent: %v\n", err)
		os.Exit(1)
	}
	defer agentHarness.Shutdown()

	fmt.Println("tenzing agent harness")
	fmt.Printf("  model  %s\n", agentHarness.GetCurrentModel())
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

func createAgent(cwd string) (*harness.Harness, error) {
	// create LLM provider with model
	modelDefinition := common.ModelDefinition{
		Name:                 "glm-5.2",
		MaxTokens:            32_768,
		ContextWindowSize:    131_072,
		DefaultContextWindow: 32_768,
	}
	llm := ollama.NewClient(ollama.Config{
		APIKey: cfg.APIKeyOllama,
		Model:  modelDefinition,
	})

	// create agent with LLM
	mainAgent, err := agent.New(agent.AgentConfig{
		Model: llm,
	})
	if err != nil {
		slog.Error("agent init failed", "error", err)
		errMsg := fmt.Errorf("agent init failed: %w", err)
		return nil, errMsg
	}

	// create harness with agent and LLM
	agentHarness, err := harness.New(harness.HarnessConfig{
		Cwd:      cwd,
		Agent:    mainAgent,
		RLMModel: llm,
	})
	if err != nil {
		slog.Error("harness init failed", "error", err, "stack", string(debug.Stack()))
		errMsg := fmt.Errorf("harness init failed: %w", err)
		return nil, errMsg
	}

	slog.Info("session started", "model", llm.GetCurrentModel(), "cwd", cwd, "tools", len(agentHarness.ToolDefinitions()))

	return agentHarness, nil
}
