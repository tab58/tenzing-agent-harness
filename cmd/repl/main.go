package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"

	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/runner"
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
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: runner.LevelTrace})))

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

	msgQueue := make(chan tea.Msg, 256)

	send := func(msg tea.Msg) {
		msgQueue <- msg
	}

	mainAgent, err := agent.New(agent.AgentConfig{
		Model: llm,
	})
	if err != nil {
		slog.Error("agent init failed", "error", err)
		fmt.Fprintf(os.Stderr, "agent init failed: %v\n", err)
		os.Exit(1)
	}

	agentHarness, err := harness.New(harness.HarnessConfig{
		Cwd:   cwd,
		Agent: mainAgent,
		OnTextDelta: func(text string) {
			send(textDeltaMsg{text: text})
		},
		OnThinkingDelta: func(text string) {
			send(thinkingDeltaMsg{text: text})
		},
		EventHooks: events.Hooks{
			OnToolExecutionStarted: func(ev events.ToolExecutionStartedEvent) {
				send(toolStartMsg{name: ev.ToolName, input: ev.Input})
			},
			OnToolSucceeded: func(ev events.ToolSucceededEvent) {
				send(toolCallMsg{name: ev.ToolName, input: ev.Input, output: ev.Output})
			},
			OnToolFailed: func(ev events.ToolFailedEvent) {
				send(toolCallMsg{name: ev.ToolName, input: ev.Input, output: ev.Error})
			},
			OnLLMResponse: func(ev events.LLMResponseEvent) {
				send(metaMsg{inputTokens: ev.InputTokens, outputTokens: ev.OutputTokens})
			},
			OnToolProgress: func(ev events.ToolProgressEvent) {
				send(toolProgressMsg{tool: ev.ToolName, phase: ev.Phase, detail: ev.Detail})
			},
		},
		MainSystemPrompt: prompts.DefaultSystemPrompt(cwd) + "\n\n" + prompts.RLMGuidance(),
		RLMModel:         llm,
	})
	if err != nil {
		slog.Error("harness init failed", "error", err, "stack", string(debug.Stack()))
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}

	m := newModel(agentHarness, llm.GetCurrentModel(), cwd, msgQueue)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
