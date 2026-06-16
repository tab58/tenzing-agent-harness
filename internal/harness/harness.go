package harness

import (
	"fmt"

	"tenzing-agent/internal/tools"
	"tenzing-agent/internal/tools/tooldef"
)

type Hooks struct {
	OnToolCall func(name string, input string, output string)
}

type Harness struct {
	agent        Agent
	toolRegistry *tools.Registry
	hooks        Hooks
	cwd          string
	systemPrompt string
}

func New(agent Agent, registry *tools.Registry, hooks Hooks, cwd string) (*Harness, error) {
	return &Harness{
		agent:        agent,
		toolRegistry: registry,
		hooks:        hooks,
		cwd:          cwd,
		systemPrompt: DefaultSystemPrompt(cwd),
	}, nil
}

func DefaultSystemPrompt(cwd string) string {
	return fmt.Sprintf(
		"You are a coding agent at %s. "+
			"Before working on any multi-step task, ALWAYS call TodoWrite first "+
			"to write your complete plan. Execute each step in order. "+
			"Call TodoUpdate after completing each step.",
		cwd,
	)
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
