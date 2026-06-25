package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"

	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(filepath.Join(cwd, ".tenzing-agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening log file: %v\n", err)
		os.Exit(1)
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

	m := newModel(agentHarness, llm.GetCurrentModel(), cwd)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
