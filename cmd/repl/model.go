package main

import (
	"context"
	"fmt"
	"strings"

	"tenzing-agent/internal/harness"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateInput state = iota
	stateRunning
)

type agentResultMsg struct {
	answer string
	err    error
}

type toolCallMsg struct {
	name   string
	input  string
	output string
}

type model struct {
	state        state
	input        string
	history      []historyEntry
	width        int
	height       int
	scrollOffset int

	mainCfg harness.AgentRunnerConfig
}

type historyEntry struct {
	role    string // "user", "assistant", "tool", "error"
	content string
}

func newModel(cfg harness.AgentRunnerConfig) model {
	return model{
		state:   stateInput,
		mainCfg: cfg,
		width:   80,
		height:  24,
	}
}

func (m model) Init() tea.Cmd {
	return tea.SetWindowTitle("tenzing repl")
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case toolCallMsg:
		m.history = append(m.history, historyEntry{
			role:    "tool",
			content: fmt.Sprintf("[%s] %s", msg.name, truncate(msg.output, 200)),
		})
		return m, nil

	case agentResultMsg:
		m.state = stateInput
		if msg.err != nil {
			m.history = append(m.history, historyEntry{role: "error", content: msg.err.Error()})
		} else {
			m.history = append(m.history, historyEntry{role: "assistant", content: msg.answer})
		}
		m.scrollToBottom()
		return m, nil
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {

	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEnter:
		if m.state != stateInput {
			return m, nil
		}
		query := strings.TrimSpace(m.input)
		m.input = ""
		if query == "" {
			return m, nil
		}
		if query == "exit" || query == "quit" || query == "q" {
			return m, tea.Quit
		}

		m.history = append(m.history, historyEntry{role: "user", content: query})
		m.state = stateRunning
		m.scrollToBottom()

		return m, m.runAgent(query)

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeyUp:
		if m.scrollOffset > 0 {
			m.scrollOffset--
		}
		return m, nil

	case tea.KeyDown:
		m.scrollOffset++
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		} else if msg.Type == tea.KeySpace {
			m.input += " "
		}
		return m, nil
	}
}

func (m *model) runAgent(query string) tea.Cmd {
	mainCfg := m.mainCfg

	return func() tea.Msg {
		if mainCfg.Agent == nil {
			return agentResultMsg{err: fmt.Errorf("no agent configured — implement harness.Agent and pass to newModel")}
		}

		h, err := harness.New(harness.HarnessConfig{
			MainRunner: mainCfg,
		})
		if err != nil {
			return agentResultMsg{err: err}
		}

		ctx := context.Background()
		var buf strings.Builder
		err = h.RunSession(ctx, strings.NewReader(query+"\n"), &buf)
		answer := strings.TrimSpace(buf.String())
		return agentResultMsg{answer: answer, err: err}
	}
}

func (m *model) scrollToBottom() {
	rendered := m.renderHistory()
	lines := strings.Count(rendered, "\n")
	viewable := m.height - 4
	if lines > viewable {
		m.scrollOffset = lines - viewable
	}
}

// --- View ---

var (
	promptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Faint(true)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	dimStyle       = lipgloss.NewStyle().Faint(true)
)

func (m model) View() string {
	var b strings.Builder

	historyView := m.renderHistory()
	lines := strings.Split(historyView, "\n")

	viewable := max(m.height-4, 1)
	start := min(m.scrollOffset, len(lines))
	end := min(start+viewable, len(lines))

	visible := lines[start:end]
	b.WriteString(strings.Join(visible, "\n"))

	for i := len(visible); i < viewable; i++ {
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	if m.state == stateRunning {
		b.WriteString(dimStyle.Render("  thinking..."))
	} else {
		b.WriteString(promptStyle.Render("> "))
		b.WriteString(m.input)
		b.WriteString("█")
	}

	return b.String()
}

func (m model) renderHistory() string {
	var b strings.Builder
	for _, entry := range m.history {
		switch entry.role {
		case "user":
			b.WriteString(userStyle.Render("❯ "+entry.content) + "\n")
		case "assistant":
			b.WriteString(assistantStyle.Render(entry.content) + "\n")
		case "tool":
			b.WriteString(toolStyle.Render("  "+entry.content) + "\n")
		case "error":
			b.WriteString(errorStyle.Render("✗ "+entry.content) + "\n")
		}
	}
	return b.String()
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
