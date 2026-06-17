package harness

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"tenzing-agent/internal/tools"
	"tenzing-agent/internal/tools/tooldef"
)

//go:embed default_system_prompt.gotmpl
var defaultSystemPromptString string

var defaultSystemPromptTmpl = template.Must(template.New("default_system_prompt").Parse(defaultSystemPromptString))

//go:embed subagent_system_prompt.gotmpl
var subagentSystemPromptString string

var subagentSystemPromptTmpl = template.Must(template.New("subagent_system_prompt").Parse(subagentSystemPromptString))

type Hooks struct {
	OnToolCall func(name string, input string, output string)
}

type Harness struct {
	mainRunner *AgentRunner
}

// ReminderBuilder produces system reminders injected before each reasoning call.
// nil means no reminders. Callers close over any state they need (e.g. cwd).
type ReminderBuilder func() []string

// RunnerFactory creates a configured AgentRunner for subagent execution.
// Receives the subagent's prompt; returns a ready-to-run runner.
type RunnerFactory func(prompt string) (*AgentRunner, error)

type HarnessConfig struct {
	Main              AgentRunnerConfig
	NewSubagentRunner RunnerFactory // nil = subagent tool not registered
}

func New(cfg HarnessConfig) (*Harness, error) {
	mainCfg := cfg.Main

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

// DefaultReminderBuilder returns a ReminderBuilder that injects todo state
// from the given working directory.
func DefaultReminderBuilder(cwd string) ReminderBuilder {
	return func() []string {
		var reminders []string
		if r := tooldef.ReadTodoReminder(cwd); r != "" {
			reminders = append(reminders, r)
		}
		return reminders
	}
}

// DefaultRunnerFactory creates a RunnerFactory from a base config.
// Each subagent gets a fresh runner with the base config.
func DefaultRunnerFactory(base AgentRunnerConfig) RunnerFactory {
	return func(prompt string) (*AgentRunner, error) {
		return NewAgentRunner(base), nil
	}
}

// DefaultMainConfig builds an AgentRunnerConfig with standard defaults:
// default system prompt, default reminder builder.
func DefaultMainConfig(agent Agent, registry *tools.Registry, hooks Hooks, cwd string) AgentRunnerConfig {
	return AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		Hooks:          hooks,
		SystemPrompt:   DefaultSystemPrompt(cwd),
		BuildReminders: DefaultReminderBuilder(cwd),
	}
}

// DefaultSubagentConfig builds an AgentRunnerConfig for subagents:
// subagent system prompt, registry without SubagentSpawn.
func DefaultSubagentConfig(agent Agent, registry *tools.Registry, hooks Hooks, cwd string) AgentRunnerConfig {
	return AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry.CopyWithout("SubagentSpawn"),
		Hooks:          hooks,
		SystemPrompt:   SubagentSystemPrompt(cwd),
		BuildReminders: DefaultReminderBuilder(cwd),
	}
}

type systemPromptData struct {
	Cwd string
}

func DefaultSystemPrompt(cwd string) string {
	var buf bytes.Buffer
	if err := defaultSystemPromptTmpl.Execute(&buf, systemPromptData{Cwd: cwd}); err != nil {
		panic("default system prompt template: " + err.Error())
	}
	return buf.String()
}

func SubagentSystemPrompt(cwd string) string {
	var buf bytes.Buffer
	if err := subagentSystemPromptTmpl.Execute(&buf, systemPromptData{Cwd: cwd}); err != nil {
		panic("subagent system prompt template: " + err.Error())
	}
	return buf.String()
}

func (h *Harness) SystemPrompt() string {
	return h.mainRunner.SystemPrompt()
}
