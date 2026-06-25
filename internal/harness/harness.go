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
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/subagent"
	"tenzing-agent/internal/harness/taskgraph"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

type Harness struct {
	mainAgentRunner *runner.AgentRunner
	toolRegistry    *tools.Registry
	skillRegistry   *skills.Registry
	todoFile        *todo.TodoFile
	hooks           runner.Hooks
}

type HarnessConfig struct {
	Agent            runner.Agent
	Hooks            runner.Hooks
	Cwd              string
	MainSystemPrompt string
	ExtraTools       []tooldef.Definition
	ExtraSkillDirs   []string
	RLMLLM           provider.LLM
	SubAgentLLM      provider.LLM
	SubAgentMaxDepth int
	SubAgentMaxIter  int
	SubAgentBuilder  runner.AgentBuilder
}

func New(cfg HarnessConfig) (*Harness, error) {
	cwd := cfg.Cwd
	if cwd == "" {
		wkdir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("could not determine default cwd: %w", err)
		}
		cwd = wkdir
	}

	mainSystemPrompt := cfg.MainSystemPrompt
	if mainSystemPrompt == "" {
		mainSystemPrompt = prompts.DefaultSystemPrompt(cwd) + "\n\n" + prompts.RLMGuidance()
	}

	todoFile := todo.NewTodoItemFile(cwd)

	taskGraph := taskgraph.NewTaskGraph(cwd)

	skillsRegistry := skills.NewRegistry()
	skillsRegistry.RegisterSkillDir("~/.claude/skills")
	for _, skillDir := range cfg.ExtraSkillDirs {
		skillsRegistry.RegisterSkillDir(skillDir)
	}

	rlmEngine, err := rlm.NewEngine(rlm.EngineConfig{
		NewFetcher: rlm.NewLLMFetcherFactory(cfg.RLMLLM),
		Querier:    rlm.NewLLMQuerier(cfg.RLMLLM),
		MaxDepth:   1,
		WorkingDir: cwd,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RLM engine: %w", err)
	}

	toolRegistry := tools.NewRegistry()
	toolRegistry.RegisterFromProvider(taskGraph)
	toolRegistry.RegisterFromProvider(skillsRegistry)
	toolRegistry.RegisterFromProvider(rlmEngine)
	toolRegistry.RegisterFromProvider(todoFile)

	if cfg.SubAgentMaxDepth > 0 && cfg.SubAgentBuilder != nil {
		subAgentLLM := cfg.SubAgentLLM
		if subAgentLLM == nil {
			subAgentLLM = cfg.RLMLLM
		}
		factory := subagent.NewSubAgentFactory(subagent.SubAgentFactoryConfig{
			AgentLLM:      subAgentLLM,
			RLMModel:      cfg.RLMLLM,
			AgentBuilder:  cfg.SubAgentBuilder,
			MaxDepth:      cfg.SubAgentMaxDepth,
			MaxIterations: cfg.SubAgentMaxIter,
			Cwd:           cwd,
		})
		toolRegistry.Register(subagent.NewSpawnAgentTool(factory))
	}

	for _, tool := range cfg.ExtraTools {
		toolRegistry.Register(tool)
	}

	hooks := cfg.Hooks

	agent := cfg.Agent
	if agent == nil {
		return nil, fmt.Errorf("agent must be defined for harness")
	}
	agent.UpdateToolDefinitions(toolRegistry.ProviderDefinitions())

	mainAgentRunner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
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
