package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/tools"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	toolRegistry := tools.NewRegistry(cwd, tools.GetDefaultToolDefs()...)
	toolProviderDefinitions := toolRegistry.ProviderDefinitions()
	hooks := harness.Hooks{}

	// build the main agent
	mainAgent := agent.New(agent.AgentConfig{
		Model:           nil,
		ToolDefinitions: toolProviderDefinitions,
	})

	agentHarness, err := harness.New(harness.HarnessConfig{
		MainRunner: harness.DefaultMainConfig(mainAgent, toolRegistry, hooks, cwd),
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
