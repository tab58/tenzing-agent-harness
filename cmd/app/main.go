package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/tools"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// start with the cwd
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	// register the tools
	toolRegistry := tools.NewRegistry(
		tools.GetDefaultToolDefs()...,
	)

	// create hooks
	hooks := harness.Hooks{}

	// create the agent
	agent := (harness.Agent)(nil)

	// create the harness
	agentHarness, err := harness.New(harness.HarnessConfig{
		Agent:        agent,
		Hooks:        hooks,
		ToolRegistry: toolRegistry,
		Cwd:          cwd,
	})
	if err != nil {
		panic(err)
	}

	// run the session
	err = agentHarness.RunSession(ctx, os.Stdin, os.Stdout)
	if err != nil {
		panic(err)
	}

	fmt.Println("Hello, World!")
}
