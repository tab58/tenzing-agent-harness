package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/tools"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	skillsRegistry := skills.NewRegistry(
		"~/.claude/skills",
	)

	toolRegistry := tools.NewRegistry(cwd,
		tools.GetDefaultToolDefs(skillsRegistry)...,
	)

	hooks := harness.Hooks{}

	// build the main agent
	mainAgent := agent.New(agent.AgentConfig{
		Model:           nil,
		ToolDefinitions: toolRegistry.ProviderDefinitions(),
	})

	// build the agent runner
	mainRunnerConfig := harness.AgentRunnerConfig{
		Agent:          mainAgent,
		ToolRegistry:   toolRegistry,
		SkillsRegistry: skillsRegistry,
		Hooks:          hooks,
		SystemPrompt:   prompts.DefaultSystemPrompt(cwd),
		BuildReminders: harness.DefaultReminderBuilder(cwd),
	}

	// build and run the harness
	agentHarness, err := harness.New(harness.HarnessConfig{
		MainRunner: mainRunnerConfig,
	})
	if err != nil {
		panic(err)
	}
	err = agentHarness.RunSession(ctx, os.Stdin, os.Stdout)
	if err != nil {
		panic(err)
	}

	fmt.Println("Hello, World!")
}
