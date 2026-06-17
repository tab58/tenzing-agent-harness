package main

import (
	"fmt"
	"os"
	"tenzing-agent/internal/harness"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// ponytail: pass nil agent — plug in your harness.Agent implementation here
	cfg := harness.HarnessConfig{
		Cwd: cwd,
	}
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
