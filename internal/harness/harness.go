package harness

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/adapters/contextstore"
	"github.com/tab58/tenzing-agent-harness/internal/adapters/toolport"
	"github.com/tab58/tenzing-agent-harness/internal/agent"
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/blackboardext"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/budgets"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/mcpext"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/permissions"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/reminders"
	"github.com/tab58/tenzing-agent-harness/internal/extensions/skillsext"
	"github.com/tab58/tenzing-agent-harness/internal/harness/advisor"
	"github.com/tab58/tenzing-agent-harness/internal/harness/blackboard"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/prompts"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/skills"
	"github.com/tab58/tenzing-agent-harness/internal/harness/subagent"
	"github.com/tab58/tenzing-agent-harness/internal/harness/todo"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools"

	"github.com/tab58/llm-providers/common"
)

type Harness struct {
	mainAgentRunner *runner.AgentRunner
	toolPort        *toolport.Composite
	todoFile        *todo.TodoFile
	eventBus        *events.EventBus
	stopHooks       func()
	stopMemoryHook  func()
	extensions      *core.Extensions
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
	skillsExt := skillsext.New(skillsRegistry)

	// The blackboard instance lives at the composition root and is SHARED:
	// the main agent and every subagent wrap the same instance in their own
	// blackboardext under their own agent ID.
	var bb *blackboard.Blackboard
	if !o.blackboardDisabled {
		bb = blackboard.New(blackboard.Config{
			Querier:    blackboard.NewLLMQuerier(blackboardLLM),
			WorkingDir: cwd,
		})
	}

	// Conversation identity: resume under the supplied ID or start fresh.
	// Pre-generated so the subagent factory (built before the runner) can
	// derive hierarchical child IDs from it: "<mainID>_<hex>".
	mainRunnerID := o.conversationID
	if mainRunnerID == "" {
		mainRunnerID = runner.NewID()
	}

	// default extensions run first, then any caller-supplied via WithExtension.
	// Permissions come FIRST so later hooks observe (and may escalate, never
	// lower) its decision.
	var defaultExts []core.Extension
	if !o.permissionsDisabled {
		policy := permissions.DefaultPolicy()
		if o.permissionPolicy != nil {
			policy = *o.permissionPolicy
		}
		defaultExts = append(defaultExts, permissions.New(policy))
	}
	defaultExts = append(defaultExts,
		reminders.New(todoFile.FormatReminder),
		skillsExt,
	)
	if o.budgetLimits != (budgets.Limits{}) {
		defaultExts = append(defaultExts, budgets.New(o.budgetLimits))
	}
	if len(o.mcpServers) > 0 {
		defaultExts = append(defaultExts, mcpext.New(o.mcpServers...))
	}
	if bb != nil {
		defaultExts = append(defaultExts, blackboardext.New(bb, "main"))
	}
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
			SkillsExt:     skillsExt,
		})
		defaultExts = append(defaultExts, subagent.NewSpawnExt(subagentFactory))
	}
	allExts := core.NewExtensions(append(defaultExts, o.extensions...)...)

	toolRegistry := tools.NewRegistry(cwd)
	toolRegistry.RegisterFromProvider(todoFile)

	// Memory: the harness owns persistence. Summaries arrive on the event
	// bus (ContextCompressedEvent) and are written per-conversation; the
	// agent layers never touch files.
	memory := newMemoryStore(mainRunnerID, time.Now)
	memory.sweep(memoryTTL, o.conversationID)

	var initialMemory string
	if o.conversationID != "" {
		initialMemory = memory.loadLatest(o.conversationID)
		if initialMemory == "" {
			slog.Warn("no memory found for conversation, starting fresh", "conversation_id", o.conversationID)
		}
	}

	stopMemoryHook := events.StartHooks(o.eventBus, events.Hooks{
		OnContextCompressed: func(e events.ContextCompressedEvent) {
			memory.persist(e.RunnerID, e.Summary)
		},
	})

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
	// Extension prompt fragments (skills index, …) are appended at the
	// composition root — the agent no longer assembles any of its prompt.
	// Registration order = fragment order (cache-stable).
	if frags := allExts.PromptFragments(); len(frags) > 0 {
		mainSystemPrompt = mainSystemPrompt + "\n\n" + strings.Join(frags, "\n\n")
	}

	// Build and wire the main agent. The default builder path is stateless;
	// custom builders own their own memory story.
	var mainAgent runner.Agent
	if o.agentBuilder != nil {
		mainAgent, err = o.agentBuilder(mainLLM, mainSystemPrompt)
	} else {
		mainAgent, err = agent.New(agent.AgentConfig{
			Model:        mainLLM,
			SystemPrompt: mainSystemPrompt,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("build main agent: %w", err)
	}

	// The composite ToolPort mounts the native registry plus any extension
	// tool sources; the loop snapshots it each turn and passes definitions
	// per reasoning call.
	composite, err := toolport.NewComposite(toolRegistry, allExts)
	if err != nil {
		return nil, fmt.Errorf("build tool port: %w", err)
	}

	// The runner owns conversation history via a ContextPort; the store
	// seeds resumed memory and emits ContextCompressedEvent on compaction.
	mainStore := contextstore.New(contextstore.Config{
		LLM:           mainLLM,
		InitialMemory: initialMemory,
		Emitter:       o.eventBus,
		RunnerID:      mainRunnerID,
	})

	// create agent runner
	mainAgentRunner, err := runner.NewAgentRunner(
		mainAgent,
		runner.WithID(mainRunnerID),
		runner.WithToolRegistry(toolRegistry),
		runner.WithToolPort(composite),
		runner.WithContextStore(mainStore),
		runner.WithEmitter(o.eventBus),
		runner.WithTextDeltaHandler(o.onTextDelta),
		runner.WithThinkingDeltaHandler(o.onThinkingDelta),
		runner.WithSystemPrompt(mainSystemPrompt),
		runner.WithExtensions(allExts),
		runner.WithApprovalTimeout(o.approvalTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize runner: %w", err)
	}

	// Session-start hooks are load-bearing: an extension that cannot start
	// (MCP connect, later) fails harness construction.
	if err := allExts.RunSessionStart(context.Background()); err != nil {
		return nil, fmt.Errorf("session start hooks: %w", err)
	}

	return &Harness{
		mainAgentRunner: mainAgentRunner,
		toolPort:        composite,
		todoFile:        todoFile,
		eventBus:        o.eventBus,
		stopHooks:       stopHooks,
		stopMemoryHook:  stopMemoryHook,
		extensions:      allExts,
	}, nil
}

func (h *Harness) Shutdown() {
	if h.stopHooks != nil {
		h.stopHooks()
	}
	if h.stopMemoryHook != nil {
		h.stopMemoryHook()
	}
	// Session-end hooks (blackboard close, …) degrade on error and get a
	// bounded window to clean up.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.extensions.RunSessionEnd(ctx)
}

func (h *Harness) GetCurrentModel() string {
	return h.mainAgentRunner.GetCurrentModel()
}

// ToolDefinitions returns the full mounted tool surface — native registry
// tools plus extension-provided tools — as the model sees it.
func (h *Harness) ToolDefinitions() []common.ToolDefinition {
	return h.toolPort.Definitions()
}

func (h *Harness) SystemPrompt() string {
	return h.mainAgentRunner.SystemPrompt()
}

// ConversationID is the main agent's ID — the handle for resuming this
// conversation later via WithConversationID.
func (h *Harness) ConversationID() string {
	return h.mainAgentRunner.ID()
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
		h.OnApprovalRequested == nil &&
		h.OnSubagentStarted == nil &&
		h.OnSubagentStopped == nil &&
		h.OnTaskCreated == nil &&
		h.OnTaskCompleted == nil
}
