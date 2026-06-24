package harness

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type Hooks struct {
	OnToolCall func(name string, input string, output string)
}

// ReminderBuilder produces system reminders injected before each reasoning call.
// nil means no reminders. Callers close over any state they need (e.g. cwd).
type ReminderBuilder func() []string

type Harness struct {
	mainRunner *AgentRunner
}

type HarnessConfig struct {
	MainRunner AgentRunnerConfig
}

func New(cfg HarnessConfig) (*Harness, error) {
	mainRunner := NewAgentRunner(cfg.MainRunner)
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
		slog.Info("user output", "len", len(answer))
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
