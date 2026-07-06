package harness

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"tenzing-agent/internal/agent"
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
	todoFile        *todo.TodoFile
	eventBus        *events.EventBus
	stopHooks       func()
}

// EventBus returns the harness event bus. It is always non-nil.
func (h *Harness) EventBus() *events.EventBus {
	return h.eventBus
}

// defaultAgentBuilder adapts the built-in agent implementation to the
// runner.AgentBuilder contract.
func defaultAgentBuilder(llm common.LLM, systemPrompt string) (runner.Agent, error) {
	return agent.New(agent.AgentConfig{Model: llm, SystemPrompt: systemPrompt})
}

func New(mainModel common.ModelDefinition, opts ...HarnessOption) (*Harness, error) {
	o := defaultHarnessOptions()
	for _, opt := range opts {
		opt(o)
	}

	brain := o.agentBuilder
	if brain == nil {
		brain = defaultAgentBuilder
	}

	factory := o.llmFactory
	if factory == nil {
		factory = defaultLLMFactory(o.baseURLs)
	}
	llms := newLLMCache(factory, o.baseURLs)

	// resolve role models: unset roles fall back to the main model
	subagentModel := o.subagentModel
	if subagentModel.Name == "" {
		subagentModel = mainModel
	}
	rlmModel := o.rlmModel
	if rlmModel.Name == "" {
		rlmModel = mainModel
	}

	mainLLM, err := llms.get(mainModel)
	if err != nil {
		return nil, fmt.Errorf("build main LLM: %w", err)
	}
	rlmLLM, err := llms.get(rlmModel)
	if err != nil {
		return nil, fmt.Errorf("build RLM LLM: %w", err)
	}

	// get cwd
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not determine default cwd: %w", err)
	}

	// register hooks to event bus
	var stopHooks func()
	if !hooksEmpty(o.hooks) {
		stopHooks = events.StartHooks(o.eventBus, o.hooks)
	}

	// set up todo file
	todoFile := todo.NewTodoFile(cwd)
	todoFile.SetEmitter(o.eventBus)

	// build skills registry
	skillsRegistry := skills.NewRegistry()
	for _, skillDir := range o.skillDirs {
		skillsRegistry.RegisterSkillDir(skillDir)
	}

	rlmProgressFn := func(ev rlm.ProgressEvent) {
		detail := ev.Output
		if ev.Phase == "repl_exec" {
			detail = ev.CodeBlock
		}
		o.eventBus.Emit(events.ToolProgressEvent{
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
		NewFetcher:        rlm.NewSimpleFetcherFactory(rlmLLM),
		Querier:           rlm.NewLLMQuerier(rlmLLM),
		MaxDepth:          1,
		WorkingDir:        cwd,
		OnProgress:        rlmProgressFn,
		DefaultIterations: o.rlmDefaultIterations,
		MaxIterations:     o.rlmMaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RLM engine: %w", err)
	}

	toolRegistry := tools.NewRegistry(cwd)
	toolRegistry.RegisterFromProvider(skillsRegistry)
	toolRegistry.RegisterFromProvider(rlmEngine)
	toolRegistry.RegisterFromProvider(todoFile)

	if o.subagentMaxDepth > 0 {
		subagentLLM, err := llms.get(subagentModel)
		if err != nil {
			return nil, fmt.Errorf("build subagent LLM: %w", err)
		}
		subagentFactory := subagent.NewSubAgentFactory(subagent.SubAgentFactoryConfig{
			AgentLLM:      subagentLLM,
			RLMModel:      rlmLLM,
			AgentBuilder:  brain,
			MaxDepth:      o.subagentMaxDepth,
			MaxIterations: o.subagentMaxIterations,
			Cwd:           cwd,
			Emitter:       o.eventBus,
		})
		toolRegistry.Register(subagent.NewSpawnAgentTool(subagentFactory))
	}

	if o.advisorModel.Name != "" {
		advisorLLM, err := llms.get(o.advisorModel)
		if err != nil {
			return nil, fmt.Errorf("build advisor LLM: %w", err)
		}
		toolRegistry.Register(advisor.NewAdvisorTool(advisorLLM))
	}

	for _, tool := range o.extraTools {
		toolRegistry.Register(tool)
	}

	if len(o.disabledTools) > 0 {
		toolRegistry = toolRegistry.CopyWithout(o.disabledTools...)
	}

	// build and wire the main agent
	mainAgent, err := brain(mainLLM, o.mainSystemPrompt)
	if err != nil {
		return nil, fmt.Errorf("build main agent: %w", err)
	}
	mainAgent.UpdateToolDefinitions(toolRegistry.ProviderDefinitions())
	mainAgent.UpdateOffloadFn(rlmEngine.Run)
	mainAgent.SetTodoProvider(todoFile.FormatReminder)

	// create agent runner
	mainAgentRunner, err := runner.NewAgentRunner(
		mainAgent,
		runner.WithToolRegistry(toolRegistry),
		runner.WithSkillsRegistry(skillsRegistry),
		runner.WithTodoFile(todoFile),
		runner.WithEmitter(o.eventBus),
		runner.WithTextDeltaHandler(o.onTextDelta),
		runner.WithThinkingDeltaHandler(o.onThinkingDelta),
		runner.WithSystemPrompt(o.mainSystemPrompt),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize runner: %w", err)
	}

	return &Harness{
		mainAgentRunner: mainAgentRunner,
		toolRegistry:    toolRegistry,
		todoFile:        todoFile,
		eventBus:        o.eventBus,
		stopHooks:       stopHooks,
	}, nil
}

func (h *Harness) Shutdown() {
	if h.stopHooks != nil {
		h.stopHooks()
	}
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
