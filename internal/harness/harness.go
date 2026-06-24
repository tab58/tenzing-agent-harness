package harness

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/rlm"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
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
	SubLMModel        provider.LLM  // nil = sub_lm tool not registered
	RLMRootModel      provider.LLM  // nil = rlm tool not registered
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

	if cfg.SubLMModel != nil {
		model := cfg.SubLMModel
		queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
			resp, err := model.SendSyncMessage(ctx, provider.CompletionRequest{
				Model:     model.GetCurrentModel(),
				System:    "Answer concisely and accurately.",
				Messages:  []provider.Message{provider.NewUserMessage(prompt)},
				MaxTokens: maxTokens,
			})
			if err != nil {
				return "", err
			}
			return resp.Text(), nil
		}
		if err := mainCfg.ToolRegistry.Register(tooldef.NewSubLMTool(queryFn)); err != nil {
			return nil, fmt.Errorf("register sub_lm tool: %w", err)
		}
		mainCfg.SystemPrompt += "\n\n" + prompts.RLMGuidance()
	}

	if cfg.RLMRootModel != nil {
		subLLM := cfg.SubLMModel
		if subLLM == nil {
			subLLM = cfg.RLMRootModel
		}
		engine, err := rlm.NewEngine(rlm.EngineConfig{
			RootLLM:    cfg.RLMRootModel,
			SubLLM:     subLLM,
			WorkingDir: mainCfg.ToolRegistry.WorkingDir(),
		})
		if err != nil {
			return nil, fmt.Errorf("create rlm engine: %w", err)
		}
		rlmRunFn := func(ctx context.Context, prompt string) (string, error) {
			return engine.Run(ctx, prompt)
		}
		if err := mainCfg.ToolRegistry.Register(tooldef.NewRLMTool(rlmRunFn)); err != nil {
			return nil, fmt.Errorf("register rlm tool: %w", err)
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
		slog.Info("user output", "len", len(answer))
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
