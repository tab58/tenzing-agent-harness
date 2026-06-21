package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"tenzing-agent/internal/agent"
	agentctx "tenzing-agent/internal/agent/context"
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

	taskGraph := agentctx.NewTaskGraph(cwd)

	skillsRegistry := skills.NewRegistry(
		"~/.claude/skills",
	)

	toolRegistry := tools.NewRegistry(cwd,
		tools.GetDefaultToolDefs(skillsRegistry, taskGraph)...,
	)

	hooks := harness.Hooks{}

	// build the main agent
	mainAgent := agent.NewWithCompressor(agent.AgentConfig{
		Model:           nil,
		ToolDefinitions: toolRegistry.ProviderDefinitions(),
	}, cwd)

	// build the agent runner
	mainRunnerConfig := harness.AgentRunnerConfig{
		Agent:          mainAgent,
		ToolRegistry:   toolRegistry,
		SkillsRegistry: skillsRegistry,
		Hooks:          hooks,
		SystemPrompt:   prompts.DefaultSystemPrompt(cwd),
		BuildReminders: harness.DefaultReminderBuilder(cwd, taskGraph),
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
