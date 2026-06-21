package harness

import (
	agentctx "tenzing-agent/internal/agent/context"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/harness/tools/tooldef"
)

// DefaultReminderBuilder returns a ReminderBuilder that injects todo state
// from the given working directory.
func DefaultReminderBuilder(cwd string, taskGraph *agentctx.TaskGraph) ReminderBuilder {
	return func() []string {
		var reminders []string
		if r := tooldef.ReadTodoReminder(cwd); r != "" {
			reminders = append(reminders, r)
		}
		if taskGraph != nil {
			if r := taskGraph.Reminder(); r != "" {
				reminders = append(reminders, r)
			}
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
func DefaultMainConfig(agent Agent, registry *tools.Registry, hooks Hooks, cwd string, taskGraph *agentctx.TaskGraph) AgentRunnerConfig {
	return AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		Hooks:          hooks,
		SystemPrompt:   prompts.DefaultSystemPrompt(cwd),
		BuildReminders: DefaultReminderBuilder(cwd, taskGraph),
	}
}

// DefaultSubagentConfig builds an AgentRunnerConfig for subagents:
// subagent system prompt, registry without SubagentSpawn.
func DefaultSubagentConfig(agent Agent, registry *tools.Registry, hooks Hooks, cwd string, taskGraph *agentctx.TaskGraph) AgentRunnerConfig {
	return AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry.CopyWithout("SubagentSpawn"),
		Hooks:          hooks,
		SystemPrompt:   prompts.SubagentSystemPrompt(cwd),
		BuildReminders: DefaultReminderBuilder(cwd, taskGraph),
	}
}
