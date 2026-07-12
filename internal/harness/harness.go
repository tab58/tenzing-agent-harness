package harness

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/tab58/tenzing-agent-harness/internal/agent"
	"github.com/tab58/tenzing-agent-harness/internal/harness/advisor"
	"github.com/tab58/tenzing-agent-harness/internal/harness/blackboard"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/prompts"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/skills"
	"github.com/tab58/tenzing-agent-harness/internal/harness/subagent"
	"github.com/tab58/tenzing-agent-harness/internal/harness/todo"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"

	"github.com/tab58/llm-providers/common"
)

type Harness struct {
	mainAgentRunner *runner.AgentRunner
	toolRegistry    *tools.Registry
	todoFile        *todo.TodoFile
	eventBus        *events.EventBus
	stopHooks       func()
	blackboard      *blackboard.Blackboard
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

	mainLLM, err := llms.get(mainModel)
	if err != nil {
		return nil, fmt.Errorf("build main LLM: %w", err)
	}

	// blackboardModel serves the shared blackboard REPL's llm_query/llm_batch
	// sub-LM calls. Only resolved when the blackboard is enabled.
	var blackboardLLM common.LLM
	if !o.blackboardDisabled {
		blackboardModel := o.blackboardModel
		if blackboardModel.Name == "" {
			blackboardModel = mainModel
		}
		blackboardLLM, err = llms.get(blackboardModel)
		if err != nil {
			return nil, fmt.Errorf("build blackboard LLM: %w", err)
		}
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

	// set up todo store
	todoFile := todo.NewTodoStore()
	todoFile.SetEmitter(o.eventBus)

	// build skills registry
	skillsRegistry := skills.NewRegistry()
	for _, skillDir := range o.skillDirs {
		skillsRegistry.RegisterSkillDir(skillDir)
	}

	toolRegistry := tools.NewRegistry(cwd)
	toolRegistry.RegisterFromProvider(skillsRegistry)
	toolRegistry.RegisterFromProvider(todoFile)

	var bb *blackboard.Blackboard
	if !o.blackboardDisabled {
		bb = blackboard.New(blackboard.Config{
			Querier:    blackboard.NewLLMQuerier(blackboardLLM),
			WorkingDir: cwd,
		})
		toolRegistry.Register(blackboard.NewREPLTool(bb, "main"))
	}

	// Pre-generated so the subagent factory (built before the runner) can
	// derive hierarchical child IDs from it: "<mainID>_<hex>".
	mainRunnerID := runner.NewID()

	if o.subagentMaxDepth > 0 {
		subagentLLM, err := llms.get(subagentModel)
		if err != nil {
			return nil, fmt.Errorf("build subagent LLM: %w", err)
		}
		subagentFactory := subagent.NewSubAgentFactory(subagent.SubAgentFactoryConfig{
			AgentLLM:      subagentLLM,
			AgentBuilder:  brain,
			MaxDepth:      o.subagentMaxDepth,
			MaxIterations: o.subagentMaxIterations,
			Cwd:           cwd,
			Emitter:       o.eventBus,
			Blackboard:    bb,
			ParentID:      mainRunnerID,
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

	// Resolve the system prompt once: the agent (wire) and the runner
	// (logging/accessor) must see the same string. Leaving it to the runner's
	// own fallback sent requests with an empty system prompt while the log
	// showed the default.
	mainSystemPrompt := o.mainSystemPrompt
	if mainSystemPrompt == "" {
		mainSystemPrompt = prompts.DefaultSystemPrompt()
	}

	// build and wire the main agent
	mainAgent, err := brain(mainLLM, mainSystemPrompt)
	if err != nil {
		return nil, fmt.Errorf("build main agent: %w", err)
	}
	mainAgent.UpdateToolDefinitions(toolRegistry.ProviderDefinitions())
	mainAgent.SetTodoProvider(todoFile.FormatReminder)

	// create agent runner
	mainAgentRunner, err := runner.NewAgentRunner(
		mainAgent,
		runner.WithID(mainRunnerID),
		runner.WithToolRegistry(toolRegistry),
		runner.WithSkillsRegistry(skillsRegistry),
		runner.WithTodoFile(todoFile),
		runner.WithEmitter(o.eventBus),
		runner.WithTextDeltaHandler(o.onTextDelta),
		runner.WithThinkingDeltaHandler(o.onThinkingDelta),
		runner.WithSystemPrompt(mainSystemPrompt),
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
		blackboard:      bb,
	}, nil
}

func (h *Harness) Shutdown() {
	if h.stopHooks != nil {
		h.stopHooks()
	}
	if h.blackboard != nil {
		_ = h.blackboard.Close()
	}
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
