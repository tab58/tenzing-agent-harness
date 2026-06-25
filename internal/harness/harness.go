package harness

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"tenzing-agent/internal/harness/prompts"
	"tenzing-agent/internal/harness/rlm"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/taskgraph"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

type Hooks struct {
	OnToolCall func(name string, input string, output string)
}

// ReminderBuilder produces system reminders injected before each reasoning call.
// nil means no reminders. Callers close over any state they need (e.g. cwd).
type ReminderBuilder func() []string

type Harness struct {
	mainAgentRunner *AgentRunner
	toolRegistry    *tools.Registry
	skillRegistry   *skills.Registry
	todoFile        *todo.TodoFile
	hooks           Hooks
}

type HarnessConfig struct {
	Agent            Agent
	Hooks            Hooks
	Cwd              string
	MainSystemPrompt string
	ExtraTools       []tooldef.Definition
	ExtraSkillDirs   []string
	RLMRootLLM       provider.LLM
}

func New(cfg HarnessConfig) (*Harness, error) {
	// set cwd
	cwd := cfg.Cwd
	if cwd == "" {
		wkdir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("could not determine default cwd: %w", err)
		}
		cwd = wkdir
	}

	// set main system prompt
	mainSystemPrompt := cfg.MainSystemPrompt
	if mainSystemPrompt == "" {
		mainSystemPrompt = prompts.DefaultSystemPrompt(cwd) + "\n\n" + prompts.RLMGuidance()
	}

	// build components for harness
	todoFile := todo.NewTodoItemFile(cwd)

	taskGraph := taskgraph.NewTaskGraph(cwd)

	skillsRegistry := skills.NewRegistry()
	skillsRegistry.RegisterSkillDir("~/.claude/skills")
	for _, skillDir := range cfg.ExtraSkillDirs {
		skillsRegistry.RegisterSkillDir(skillDir)
	}

	rlmEngine, err := rlm.NewEngine(rlm.EngineConfig{
		RootLLM:    cfg.RLMRootLLM,
		SubLLM:     cfg.RLMRootLLM,
		MaxDepth:   1,
		WorkingDir: cwd,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RLM engine: %w", err)
	}

	// build tool registry (check to see if the builtins are created in registered tools)
	toolRegistry := tools.NewRegistry()
	toolRegistry.RegisterFromProvider(taskGraph)
	toolRegistry.RegisterFromProvider(skillsRegistry)
	toolRegistry.RegisterFromProvider(rlmEngine)
	toolRegistry.RegisterFromProvider(todoFile)
	for _, tool := range cfg.ExtraTools {
		toolRegistry.Register(tool)
	}

	// get hooks
	hooks := cfg.Hooks

	// configure agent
	agent := cfg.Agent
	if agent == nil {
		return nil, fmt.Errorf("agent must be defined for harness")
	}
	agent.UpdateToolDefinitions(toolRegistry.ProviderDefinitions())

	mainAgentRunner, err := NewAgentRunner(AgentRunnerConfig{
		ToolRegistry:   toolRegistry,
		SkillsRegistry: skillsRegistry,
		TodoFile:       todoFile,
		TaskGraph:      taskGraph,
		Agent:          agent,
		Hooks:          hooks,
		SystemPrompt:   mainSystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize runner: %w", err)
	}

	return &Harness{
		mainAgentRunner: mainAgentRunner,
		toolRegistry:    toolRegistry,
		skillRegistry:   skillsRegistry,
		todoFile:        todoFile,
		hooks:           cfg.Hooks,
	}, nil
}

func (h *Harness) ToolDefinitions() []tooldef.Definition {
	return h.toolRegistry.Definitions()
}

func (h *Harness) SystemPrompt() string {
	return h.mainAgentRunner.SystemPrompt()
}

func (h *Harness) RunSession(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("context canceled: %w", ctx.Err())
		}

		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if query == "q" || query == "exit" {
			break
		}

		answer, err := h.mainAgentRunner.RunLoop(ctx, query)
		if err != nil {
			return fmt.Errorf("agent loop error: %w", err)
		}
		slog.Info("user output", "len", len(answer))
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
