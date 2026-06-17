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

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	toolRegistry := tools.NewRegistry(cwd, tools.GetDefaultToolDefs()...)
	hooks := harness.Hooks{}

	// plug in your harness.Agent implementation here
	agent := (harness.Agent)(nil)

	mainCfg := harness.DefaultMainConfig(agent, toolRegistry, hooks, cwd)

	agentHarness, err := harness.New(harness.HarnessConfig{
		Main: mainCfg,
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
