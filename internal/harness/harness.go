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
	"time"

	"tenzing-agent/internal/harness/advisor"
	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/rlm"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/subagent"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/harness/tools/tooldef"

	"github.com/tab58/llm-providers/common"
)

type Harness struct {
	mainAgentRunner *runner.AgentRunner
	toolRegistry    *tools.Registry
	skillRegistry   *skills.Registry
	todoFile        *todo.TodoFile
	eventBus        *events.EventBus
}

// EventBus returns the harness event bus. It is always non-nil.
func (h *Harness) EventBus() *events.EventBus {
	return h.eventBus
}

type HarnessConfig struct {
	Agent runner.Agent

	// OnTextDelta is called with incremental text output from the agent.
	// It is called for each text delta, which may be a single character or
	// a larger chunk of text. The callback is called from the agent's
	// goroutine, so it should not block for long.
	OnTextDelta func(string)

	// OnThinkingDelta is called with incremental thinking output from the
	// agent. It is called for each thinking delta, which may be a single
	// character or a larger chunk of text. The callback is called from the
	// agent's goroutine, so it should not block for long.
	OnThinkingDelta func(string)

	EventBus         *events.EventBus
	EventHooks       events.Hooks
	Cwd              string
	MainSystemPrompt string
	ExtraTools       []tooldef.Definition
	// DisabledTools removes tools by name (case-insensitive) after all
	// registration, including built-ins like "bash" and "edit".
	DisabledTools  []string
	ExtraSkillDirs []string
	// RLMModel is the LLM used for reasoning and offloading. It is used
	// for the main agent and sub-agents unless SubAgentLLM is set.
	RLMModel             common.LLM
	RLMDefaultIterations int
	RLMMaxIterations     int
	// AdvisorModel backs the "advisor" tool. It should be a stronger
	// reasoning model than the main agent's. The tool is registered only
	// when EnableAdvisor is also true — disabled by default while the
	// tool is being improved.
	AdvisorModel  common.LLM
	EnableAdvisor bool
	// SubAgentLLM is the LLM used for sub-agents spawned by the main agent.
	SubAgentLLM      common.LLM
	SubAgentMaxDepth int
	SubAgentMaxIter  int
	SubAgentBuilder  runner.AgentBuilder
}

// hooksEmpty reports whether no hook callbacks are set in h.
func hooksEmpty(h events.Hooks) bool {
	return h.OnSessionStarted == nil &&
		h.OnSessionEnded == nil &&
		h.OnTurnStarted == nil &&
		h.OnTurnCompleted == nil &&
		h.OnLoopStarted == nil &&
		h.OnLoopStopped == nil &&
		h.OnReasoningStarted == nil &&
		h.OnReasoningFinished == nil &&
		h.OnToolExecutionStarted == nil &&
		h.OnToolExecutionFinished == nil &&
		h.OnLLMResponse == nil &&
		h.OnToolSucceeded == nil &&
		h.OnToolFailed == nil &&
		h.OnToolProgress == nil &&
		h.OnContextCompressing == nil &&
		h.OnContextCompressed == nil &&
		h.OnError == nil &&
		h.OnSubagentStarted == nil &&
		h.OnSubagentStopped == nil &&
		h.OnTaskCreated == nil &&
		h.OnTaskCompleted == nil
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

	bus := cfg.EventBus
	if bus == nil {
		bus = events.NewEventBus()
	}
	if !hooksEmpty(cfg.EventHooks) {
		events.NewHooksAdapter(bus, cfg.EventHooks)
	}

	todoFile := todo.NewTodoFile(cwd)
	todoFile.SetEmitter(bus)

	skillsRegistry := skills.NewRegistry()
	skillsRegistry.RegisterSkillDir("~/.claude/skills")
	for _, skillDir := range cfg.ExtraSkillDirs {
		skillsRegistry.RegisterSkillDir(skillDir)
	}

	rlmProgressFn := func(ev rlm.ProgressEvent) {
		detail := ev.Output
		if ev.Phase == "repl_exec" {
			detail = ev.CodeBlock
		}
		bus.Emit(events.ToolProgressEvent{
			BaseEvent: events.NewBaseEvent(events.EventToolProgress, ""),
			ToolName:  "rlm",
			Phase:     ev.Phase,
			Detail:    detail,
			Iteration: ev.Iteration,
			TokensIn:  ev.TokensIn,
			TokensOut: ev.TokensOut,
		})
	}

	rlmEngine, err := rlm.NewEngine(rlm.EngineConfig{
		NewFetcher:        rlm.NewLLMFetcherFactory(cfg.RLMModel),
		Querier:           rlm.NewLLMQuerier(cfg.RLMModel),
		MaxDepth:          1,
		WorkingDir:        cwd,
		OnProgress:        rlmProgressFn,
		DefaultIterations: cfg.RLMDefaultIterations,
		MaxIterations:     cfg.RLMMaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RLM engine: %w", err)
	}

	toolRegistry := tools.NewRegistry(cwd)
	toolRegistry.RegisterFromProvider(skillsRegistry)
	toolRegistry.RegisterFromProvider(rlmEngine)
	toolRegistry.RegisterFromProvider(todoFile)

	if cfg.SubAgentMaxDepth > 0 && cfg.SubAgentBuilder != nil {
		subAgentLLM := cfg.SubAgentLLM
		if subAgentLLM == nil {
			subAgentLLM = cfg.RLMModel
		}
		factory := subagent.NewSubAgentFactory(subagent.SubAgentFactoryConfig{
			AgentLLM:      subAgentLLM,
			RLMModel:      cfg.RLMModel,
			AgentBuilder:  cfg.SubAgentBuilder,
			MaxDepth:      cfg.SubAgentMaxDepth,
			MaxIterations: cfg.SubAgentMaxIter,
			Cwd:           cwd,
			Emitter:       bus,
		})
		toolRegistry.Register(subagent.NewSpawnAgentTool(factory))
	}

	if cfg.EnableAdvisor && cfg.AdvisorModel != nil {
		toolRegistry.Register(advisor.NewAdvisorTool(cfg.AdvisorModel))
	}

	for _, tool := range cfg.ExtraTools {
		toolRegistry.Register(tool)
	}

	if len(cfg.DisabledTools) > 0 {
		toolRegistry = toolRegistry.CopyWithout(cfg.DisabledTools...)
	}

	// validate and set up agent
	agent := cfg.Agent
	if agent == nil {
		return nil, fmt.Errorf("agent must be defined for harness")
	}
	agent.UpdateToolDefinitions(toolRegistry.ProviderDefinitions())
	agent.UpdateOffloadFn(rlmEngine.Run)
	agent.SetTodoProvider(todoFile.FormatReminder)

	// create agent runner
	mainAgentRunner, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(toolRegistry),
		runner.WithSkillsRegistry(skillsRegistry),
		runner.WithTodoFile(todoFile),
		runner.WithEmitter(bus),
		runner.WithTextDeltaHandler(cfg.OnTextDelta),
		runner.WithThinkingDeltaHandler(cfg.OnThinkingDelta),
		runner.WithSystemPrompt(mainSystemPrompt),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize runner: %w", err)
	}

	return &Harness{
		mainAgentRunner: mainAgentRunner,
		toolRegistry:    toolRegistry,
		skillRegistry:   skillsRegistry,
		todoFile:        todoFile,
		eventBus:        bus,
	}, nil
}

func (h *Harness) Shutdown() {
	h.todoFile.Cleanup()
}

func (h *Harness) GetCurrentModel() string {
	return h.mainAgentRunner.GetCurrentModel()
}

func (h *Harness) ToolDefinitions() []tooldef.Definition {
	return h.toolRegistry.Definitions()
}

func (h *Harness) SystemPrompt() string {
	return h.mainAgentRunner.SystemPrompt()
}

func (h *Harness) RunTurn(ctx context.Context, query string) (string, error) {
	return h.mainAgentRunner.RunLoop(ctx, query)
}

func (h *Harness) RunSession(ctx context.Context, in io.Reader, out io.Writer) error {
	sessionStart := time.Now()
	turnCount := 0
	h.eventBus.Emit(events.SessionStartedEvent{
		BaseEvent: events.NewBaseEvent(events.EventSessionStarted, ""),
	})
	defer func() {
		h.eventBus.Emit(events.SessionEndedEvent{
			BaseEvent: events.NewBaseEvent(events.EventSessionEnded, ""),
			TurnCount: turnCount,
			Duration:  time.Since(sessionStart).Round(time.Millisecond),
		})
	}()

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
		turnCount++
		slog.Info("user output", "len", len(answer))
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
