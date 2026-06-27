package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"tenzing-agent/internal/harness"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateInput state = iota
	stateRunning
)

const (
	inputAreaHeight = 2
	maxQueryHistory = 100
)

// --- Messages ---

type agentResultMsg struct {
	answer string
	err    error
}

type toolStartMsg struct {
	name  string
	input string
}

type toolCallMsg struct {
	name   string
	input  string
	output string
}

type metaMsg struct {
	inputTokens  int64
	outputTokens int64
}

type headerReadyMsg struct{}

// --- Model ---

type historyEntry struct {
	role    string
	content string
}

type model struct {
	state    state
	input    textinput.Model
	viewport viewport.Model
	spinner  spinner.Model
	history  []historyEntry
	width    int
	height   int

	queryHistory []string
	historyIdx   int
	savedInput   string

	totalInputTokens  int64
	totalOutputTokens int64

	agentHarness *harness.Harness
	cancelFn     context.CancelFunc
	modelName    string
	cwd          string
	toolCount    int
}

func newModel(h *harness.Harness, modelName, cwd string) model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 0
	ti.PromptStyle = promptStyle
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	vp := viewport.New(80, 20)

	return model{
		state:        stateInput,
		input:        ti,
		viewport:     vp,
		spinner:      sp,
		agentHarness: h,
		modelName:    modelName,
		cwd:          cwd,
		toolCount:    len(h.ToolDefinitions()),
		width:        80,
		height:       24,
		historyIdx:   -1,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("tenzing repl"),
		textinput.Blink,
		func() tea.Msg { return headerReadyMsg{} },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = max(msg.Height-inputAreaHeight, 1)
		m.input.Width = max(msg.Width-4, 10)
		m.refreshViewport()
		return m, nil

	case headerReadyMsg:
		m.history = append(m.history, historyEntry{
			role:    "header",
			content: m.buildHeader(),
		})
		m.refreshViewport()
		return m, nil

	case spinner.TickMsg:
		if m.state == stateRunning {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case toolStartMsg:
		m.history = append(m.history, historyEntry{
			role:    "tool_start",
			content: msg.name,
		})
		m.refreshViewport()
		return m, nil

	case toolCallMsg:
		m.history = append(m.history, historyEntry{
			role:    "tool",
			content: fmt.Sprintf("%s %s", msg.name, truncate(msg.output, 200)),
		})
		m.refreshViewport()
		return m, nil

	case metaMsg:
		m.totalInputTokens += msg.inputTokens
		m.totalOutputTokens += msg.outputTokens
		return m, nil

	case agentResultMsg:
		m.state = stateInput
		m.cancelFn = nil
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.history = append(m.history, historyEntry{role: "system", content: "interrupted"})
			} else {
				m.history = append(m.history, historyEntry{role: "error", content: msg.err.Error()})
			}
		} else if msg.answer != "" {
			m.history = append(m.history, historyEntry{role: "assistant", content: msg.answer})
		}
		m.refreshViewport()
		m.input.Focus()
		return m, textinput.Blink

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		if m.state == stateRunning && m.cancelFn != nil {
			m.cancelFn()
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyCtrlD:
		if m.state == stateInput && m.input.Value() == "" {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case tea.KeyCtrlL:
		if m.state == stateInput {
			return m.clearHistory()
		}
		return m, nil

	case tea.KeyEnter:
		if m.state != stateInput {
			return m, nil
		}
		query := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		if query == "" {
			return m, nil
		}
		if query == "exit" || query == "quit" || query == "q" {
			return m, tea.Quit
		}
		if strings.HasPrefix(query, "/") {
			return m.handleSlashCommand(query)
		}

		m.queryHistory = append([]string{query}, m.queryHistory...)
		if len(m.queryHistory) > maxQueryHistory {
			m.queryHistory = m.queryHistory[:maxQueryHistory]
		}
		m.historyIdx = -1
		m.savedInput = ""

		m.history = append(m.history, historyEntry{role: "user", content: query})
		m.state = stateRunning
		m.input.Blur()

		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFn = cancel

		m.refreshViewport()
		return m, tea.Batch(m.runAgent(ctx, query), m.spinner.Tick)

	case tea.KeyUp:
		if m.state == stateInput {
			m.navigateHistoryUp()
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyDown:
		if m.state == stateInput {
			m.navigateHistoryDown()
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	default:
		if m.state == stateInput {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

func (m *model) navigateHistoryUp() {
	if len(m.queryHistory) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.savedInput = m.input.Value()
		m.historyIdx = 0
	} else if m.historyIdx < len(m.queryHistory)-1 {
		m.historyIdx++
	} else {
		return
	}
	m.input.SetValue(m.queryHistory[m.historyIdx])
	m.input.SetCursor(len(m.queryHistory[m.historyIdx]))
}

func (m *model) navigateHistoryDown() {
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx > 0 {
		m.historyIdx--
		m.input.SetValue(m.queryHistory[m.historyIdx])
		m.input.SetCursor(len(m.queryHistory[m.historyIdx]))
	} else {
		m.historyIdx = -1
		m.input.SetValue(m.savedInput)
		m.input.SetCursor(len(m.savedInput))
	}
}

func (m model) clearHistory() (tea.Model, tea.Cmd) {
	m.history = []historyEntry{{role: "header", content: m.buildHeader()}}
	m.refreshViewport()
	return m, nil
}

func (m model) handleSlashCommand(cmd string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "/clear", "/c":
		return m.clearHistory()
	case "/help", "/h":
		m.history = append(m.history, historyEntry{
			role:    "system",
			content: slashCommandHelp,
		})
		m.refreshViewport()
		return m, nil
	case "/tokens", "/t":
		info := fmt.Sprintf("input: %s  output: %s  total: %s",
			formatTokenCount(m.totalInputTokens),
			formatTokenCount(m.totalOutputTokens),
			formatTokenCount(m.totalInputTokens+m.totalOutputTokens),
		)
		m.history = append(m.history, historyEntry{role: "system", content: info})
		m.refreshViewport()
		return m, nil
	case "/exit":
		return m, tea.Quit
	default:
		m.history = append(m.history, historyEntry{
			role:    "error",
			content: fmt.Sprintf("unknown command: %s (type /help for commands)", cmd),
		})
		m.refreshViewport()
		return m, nil
	}
}

const slashCommandHelp = `commands:
  /clear, /c    clear chat history
  /help, /h     show this help
  /tokens, /t   show token usage
  /exit         exit the REPL
  ctrl+l        clear chat history
  ctrl+c        cancel running / exit
  ctrl+d        exit (empty input)`

func (m model) runAgent(ctx context.Context, query string) tea.Cmd {
	h := m.agentHarness
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("agent panic", "error", r)
				msg = agentResultMsg{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		answer, err := h.RunTurn(ctx, query)
		return agentResultMsg{answer: answer, err: err}
	}
}

func (m *model) refreshViewport() {
	m.viewport.SetContent(m.renderHistory())
	m.viewport.GotoBottom()
}

// --- View ---

var (
	promptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Faint(true)
	toolStartStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	systemStyle    = lipgloss.NewStyle().Faint(true)
	separatorStyle = lipgloss.NewStyle().Faint(true)
	headerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	headerDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m model) buildHeader() string {
	var b strings.Builder
	b.WriteString("tenzing agent harness\n")
	b.WriteString(fmt.Sprintf("  model    %s\n", m.modelName))
	b.WriteString(fmt.Sprintf("  cwd      %s\n", m.cwd))
	b.WriteString(fmt.Sprintf("  tools    %d registered\n", m.toolCount))
	b.WriteString(fmt.Sprintf("  platform %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString("  exit     q / exit / ctrl+c / ctrl+d")
	return b.String()
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderSeparator())
	b.WriteString("\n")

	if m.state == stateRunning {
		b.WriteString(fmt.Sprintf("  %s working...", m.spinner.View()))
	} else {
		b.WriteString(m.input.View())
	}

	return b.String()
}

func (m model) renderSeparator() string {
	tokens := m.totalInputTokens + m.totalOutputTokens
	if tokens == 0 {
		return separatorStyle.Render(strings.Repeat("─", m.width))
	}
	info := fmt.Sprintf(" %s · %s↑ %s↓ ",
		m.modelName,
		formatTokenCount(m.totalInputTokens),
		formatTokenCount(m.totalOutputTokens),
	)
	infoWidth := lipgloss.Width(info)
	remaining := m.width - infoWidth
	if remaining < 4 {
		return separatorStyle.Render(strings.Repeat("─", m.width))
	}
	left := remaining / 2
	right := remaining - left
	return separatorStyle.Render(strings.Repeat("─", left) + info + strings.Repeat("─", right))
}

func (m model) renderHistory() string {
	var b strings.Builder
	w := m.width - 2
	if w < 20 {
		w = 20
	}

	for _, entry := range m.history {
		switch entry.role {
		case "header":
			lines := strings.Split(entry.content, "\n")
			b.WriteString(headerStyle.Render(lines[0]) + "\n")
			for _, line := range lines[1:] {
				b.WriteString(headerDim.Render(line) + "\n")
			}
			b.WriteString("\n")
		case "user":
			b.WriteString(userStyle.Width(w).Render("❯ "+entry.content) + "\n")
		case "assistant":
			b.WriteString(renderMarkdown(entry.content, w))
			b.WriteString("\n")
		case "tool_start":
			b.WriteString(toolStartStyle.Render("  ⚙ "+entry.content) + "\n")
		case "tool":
			b.WriteString(toolStyle.Width(w).Render("  ✓ "+entry.content) + "\n")
		case "error":
			b.WriteString(errorStyle.Width(w).Render("✗ "+entry.content) + "\n")
		case "system":
			b.WriteString(systemStyle.Render("  "+entry.content) + "\n")
		}
	}
	return b.String()
}

func renderMarkdown(content string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	rendered, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(rendered, "\n")
}

func formatTokenCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
