package harness

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"tenzing-agent/internal/harness/tools/tooldef"
)

type Hooks struct {
	OnToolCall func(name string, input string, output string)
}

// ReminderBuilder produces system reminders injected before each reasoning call.
// nil means no reminders. Callers close over any state they need (e.g. cwd).
type ReminderBuilder func() []string

// RunnerFactory creates a configured AgentRunner for subagent execution.
// Receives the subagent's prompt; returns a ready-to-run runner.
type RunnerFactory func(prompt string) (*AgentRunner, error)

type Harness struct {
	mainRunner *AgentRunner
}

type HarnessConfig struct {
	MainRunner        AgentRunnerConfig
	NewSubagentRunner RunnerFactory // nil = subagent tool not registered
}

func New(cfg HarnessConfig) (*Harness, error) {
	mainCfg := cfg.MainRunner

	if cfg.NewSubagentRunner != nil {
		spawnFn := func(ctx context.Context, prompt string) (string, error) {
			runner, err := cfg.NewSubagentRunner(prompt)
			if err != nil {
				return "", fmt.Errorf("create subagent runner: %w", err)
			}
			return runner.RunLoop(ctx, prompt)
		}
		if err := mainCfg.ToolRegistry.Register(tooldef.NewSubagentTool(spawnFn)); err != nil {
			return nil, fmt.Errorf("register subagent tool: %w", err)
		}
	}

	mainRunner := NewAgentRunner(mainCfg)

	return &Harness{
		mainRunner: mainRunner,
	}, nil
}

func (h *Harness) SystemPrompt() string {
	return h.mainRunner.SystemPrompt()
}

func (h *Harness) RunSession(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("context canceled: %w", ctx.Err())
		}

		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if query == "q" || query == "exit" {
			break
		}

		answer, err := h.mainRunner.RunLoop(ctx, query)
		if err != nil {
			return fmt.Errorf("agent loop error: %w", err)
		}
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
