package main

import (
	"fmt"
	"os"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/tools"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	skillsRegistry := skills.NewRegistry(
		"~/.claude/skills",
	)
	toolRegistry := tools.NewRegistry(cwd,
		tools.GetDefaultToolDefs(skillsRegistry)...,
	)
	hooks := harness.Hooks{}

	// plug in your harness.Agent implementation here
	agent := (harness.Agent)(nil)

	mainCfg := harness.DefaultMainConfig(agent, toolRegistry, hooks, cwd)

	m := newModel(mainCfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
