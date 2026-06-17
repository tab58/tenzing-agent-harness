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
	agent        Agent
	toolRegistry *tools.Registry
	hooks        Hooks
	cwd          string
	systemPrompt string
	mainRunner   *AgentRunner
}

type AgentFactory func() Agent

type HarnessConfig struct {
	Agent        Agent
	ToolRegistry *tools.Registry
	Hooks        Hooks
	Cwd          string
	NewAgent     AgentFactory
}

func New(cfg HarnessConfig) (*Harness, error) {
	agent := cfg.Agent
	registry := cfg.ToolRegistry
	hooks := cfg.Hooks
	cwd := cfg.Cwd
	newAgent := cfg.NewAgent

	subRegistry := registry.CopyWithout("SubagentSpawn")

	spawnFn := func(ctx context.Context, prompt string) (string, error) {
		if newAgent == nil {
			return "", fmt.Errorf("no agent factory configured")
		}
		subAgent := newAgent()
		subRunner := NewAgentRunner(AgentRunnerConfig{
			Agent:        subAgent,
			ToolRegistry: subRegistry,
			Hooks:        hooks,
			Cwd:          cwd,
			SystemPrompt: SubagentSystemPrompt(cwd),
		})
		return subRunner.RunLoop(ctx, prompt)
	}

	if err := registry.Register(tooldef.NewSubagentTool(spawnFn)); err != nil {
		return nil, fmt.Errorf("register subagent tool: %w", err)
	}

	mainRunner := NewAgentRunner(AgentRunnerConfig{
		Agent:        agent,
		ToolRegistry: registry,
		Hooks:        hooks,
		Cwd:          cwd,
		SystemPrompt: DefaultSystemPrompt(cwd),
	})

	return &Harness{
		agent:        agent,
		toolRegistry: registry,
		hooks:        hooks,
		cwd:          cwd,
		systemPrompt: DefaultSystemPrompt(cwd),
		mainRunner:   mainRunner,
	}, nil
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
	return h.systemPrompt
}

func (h *Harness) buildSystemReminders() []string {
	var reminders []string
	if r := tooldef.ReadTodoReminder(h.cwd); r != "" {
		reminders = append(reminders, r)
	}
	return reminders
}
