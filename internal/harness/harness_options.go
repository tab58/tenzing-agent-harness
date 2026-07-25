package harness

import (
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/adapters/eventbus"
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/core/tooldef"
	"github.com/tab58/tenzing-agent-harness/internal/features/budgets"
	"github.com/tab58/tenzing-agent-harness/internal/features/mcp"
	"github.com/tab58/tenzing-agent-harness/internal/features/permissions"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"

	"github.com/tab58/llm-providers/common"
)

type harnessOptions struct {
	// agentBuilder builds the Agent ("brain") for the main loop and
	// subagents. Nil means the default agent implementation.
	agentBuilder runner.AgentBuilder

	// llmFactory builds LLM clients from model definitions. Nil means the
	// default env-var-based factory (provider.LLMFromEnv). A custom factory
	// is responsible for its own base URLs; baseURLs only feeds the default.
	llmFactory func(common.ModelDefinition) (common.LLM, error)

	// baseURLs holds per-provider base URL overrides consumed by the
	// default LLM factory (e.g. a remote Ollama host).
	baseURLs map[common.Provider]string

	// subagentModel is the model for spawned subagents. Zero value falls
	// back to the harness main model.
	subagentModel common.ModelDefinition

	// subagentMaxDepth limits subagent nesting; 0 disables the spawn_agent
	// tool entirely.
	subagentMaxDepth int

	// subagentMaxIterations caps loop iterations per subagent.
	subagentMaxIterations int

	// blackboardModel is the model used for llm_query/llm_batch sub-LM calls
	// inside the shared blackboard REPL; unset falls back to the main model.
	blackboardModel common.ModelDefinition

	// advisorModel enables the advisor tool when set (non-zero Name).
	advisorModel common.ModelDefinition

	// onTextDelta is called with incremental text output from the agent,
	// tagged with the emitting runner's id. It is called from the agent's
	// goroutine, so it should not block.
	onTextDelta func(runnerID, text string)

	// onThinkingDelta is called with incremental thinking output from the
	// agent, tagged with the emitting runner's id. Called from the agent's
	// goroutine, so it should not block.
	onThinkingDelta func(runnerID, text string)

	// eventBus is an event bus that transports all events
	eventBus *eventbus.EventBus

	// mainSystemPrompt is the system prompt of the main agent
	mainSystemPrompt string

	// conversationID resumes a prior conversation: the main agent runs
	// under this ID and its latest memory file is loaded at startup.
	// Empty starts a fresh conversation under a random ID.
	conversationID string

	// hooks holds optional typed callbacks dispatched from the event
	// bus. Only set hooks fire; leave the rest nil.
	hooks eventbus.Hooks

	// extraTools are additional tools to add the registry
	extraTools map[string]tooldef.Definition

	// skillDirs are directories to find skills in
	skillDirs []string

	// disabledTools removes tools by name (case-insensitive) after all
	// registration, including built-ins like "bash" and "edit".
	disabledTools []string

	// blackboardDisabled turns off the shared blackboard REPL and its
	// repl tool (enabled by default).
	blackboardDisabled bool

	// extensions are additional core.Extension registrations, appended after
	// the default extensions (e.g. reminders). Order of WithExtension calls
	// is hook execution order.
	extensions []core.Extension

	// permissionPolicy overrides the default tool permission policy.
	// Nil means permissions.DefaultPolicy().
	permissionPolicy *permissions.Policy

	// permissionsDisabled skips registering the permissions extension
	// entirely (explicit opt-out for headless/trusted drivers).
	permissionsDisabled bool

	// approvalTimeout bounds how long an AskUser tool call waits for an
	// approval response before being denied.
	approvalTimeout time.Duration

	// budgetLimits, when non-zero, registers the budgets extension for the
	// main loop (graceful termination on iteration/wall-clock/token caps).
	budgetLimits budgets.Limits

	// mcpServers are external MCP servers to mount as dynamic tool sources.
	// The mcp extension is registered only when at least one is configured.
	mcpServers []mcp.ServerConfig
}

func defaultHarnessOptions() *harnessOptions {
	return &harnessOptions{
		eventBus: eventbus.NewEventBus(),
		skillDirs: []string{
			"~/.claude/skills",
		},
		extraTools:            make(map[string]tooldef.Definition),
		baseURLs:              make(map[common.Provider]string),
		subagentMaxDepth:      1,
		subagentMaxIterations: 100,
		approvalTimeout:       120 * time.Second,
	}
}

type HarnessOption func(*harnessOptions)

// WithAgentBuilder overrides how the Agent ("brain") is built from an LLM
// and system prompt. The default uses the built-in agent implementation.
func WithAgentBuilder(builder runner.AgentBuilder) HarnessOption {
	return func(o *harnessOptions) {
		o.agentBuilder = builder
	}
}

// WithLLMFactory overrides how LLM clients are built from model
// definitions. The default resolves API keys from provider env vars.
func WithLLMFactory(factory func(common.ModelDefinition) (common.LLM, error)) HarnessOption {
	return func(o *harnessOptions) {
		o.llmFactory = factory
	}
}

// WithProviderBaseURL sets the base URL the default LLM factory uses for the
// given provider. Ignored when WithLLMFactory is set.
func WithProviderBaseURL(p common.Provider, url string) HarnessOption {
	return func(o *harnessOptions) {
		o.baseURLs[p] = url
	}
}

// WithSubagentModel sets the model used for spawned subagents. Unset falls
// back to the main model.
func WithSubagentModel(model common.ModelDefinition) HarnessOption {
	return func(o *harnessOptions) {
		o.subagentModel = model
	}
}

// WithSubagentDepth sets the maximum subagent nesting depth. 0 disables the
// spawn_agent tool.
func WithSubagentDepth(depth int) HarnessOption {
	return func(o *harnessOptions) {
		o.subagentMaxDepth = depth
	}
}

func WithSubagentMaxIterations(maxIter int) HarnessOption {
	return func(o *harnessOptions) {
		o.subagentMaxIterations = maxIter
	}
}

// WithBlackboardModel sets the model used for llm_query/llm_batch calls
// inside the shared blackboard REPL; unset falls back to the main model.
// These are stateless one-shot completions (no tools, no agent loop) —
// not subagents — so a small/fast model is often the right choice.
func WithBlackboardModel(model common.ModelDefinition) HarnessOption {
	return func(o *harnessOptions) {
		o.blackboardModel = model
	}
}

// WithAdvisorModel enables the advisor tool using the given model. Without
// this option the advisor tool is not registered.
func WithAdvisorModel(model common.ModelDefinition) HarnessOption {
	return func(o *harnessOptions) {
		o.advisorModel = model
	}
}

func WithDisabledTool(toolName string) HarnessOption {
	return func(o *harnessOptions) {
		o.disabledTools = append(o.disabledTools, toolName)
	}
}

// WithSkillsDir registers an additional skills directory. Nonexistent or
// unreadable directories are skipped at discovery time.
func WithSkillsDir(dir string) HarnessOption {
	return func(o *harnessOptions) {
		o.skillDirs = append(o.skillDirs, dir)
	}
}

func WithTool(tool tooldef.Definition) HarnessOption {
	return func(o *harnessOptions) {
		name := tool.Name()
		extraTools := o.extraTools
		if _, ok := extraTools[name]; !ok {
			extraTools[name] = tool
		}
	}
}

func WithHooks(hooks eventbus.Hooks) HarnessOption {
	return func(o *harnessOptions) {
		o.hooks = hooks
	}
}

func WithSystemPrompt(prompt string) HarnessOption {
	return func(o *harnessOptions) {
		o.mainSystemPrompt = prompt
	}
}

// WithConversationID resumes a prior conversation: the main agent runs under
// this ID and its latest memory file is loaded as initial context. The
// caller owns ID uniqueness across live processes.
func WithConversationID(id string) HarnessOption {
	return func(o *harnessOptions) {
		o.conversationID = id
	}
}

func WithEventBus(bus *eventbus.EventBus) HarnessOption {
	return func(o *harnessOptions) {
		o.eventBus = bus
	}
}

// WithTextDeltaHandler registers a callback for incremental text output.
// runnerID identifies the emitting runner so multiplexed consumers (RPC
// mode) can correlate deltas with their turn.
func WithTextDeltaHandler(f func(runnerID, text string)) HarnessOption {
	return func(o *harnessOptions) {
		o.onTextDelta = f
	}
}

// WithThinkingDeltaHandler registers a callback for incremental thinking
// output, tagged with the emitting runner's id like WithTextDeltaHandler.
func WithThinkingDeltaHandler(f func(runnerID, text string)) HarnessOption {
	return func(o *harnessOptions) {
		o.onThinkingDelta = f
	}
}

// WithBlackboardDisabled turns off the shared blackboard REPL. The repl
// tool is not registered and subagent results are always returned inline.
func WithBlackboardDisabled() HarnessOption {
	return func(o *harnessOptions) {
		o.blackboardDisabled = true
	}
}

// WithExtension registers an additional core extension. Order of WithExtension
// calls is hook execution order (after the default extensions).
func WithExtension(ext core.Extension) HarnessOption {
	return func(o *harnessOptions) { o.extensions = append(o.extensions, ext) }
}

// WithPermissionPolicy replaces the default tool permission policy
// (permissions.DefaultPolicy: ask for code-executing/file-writing tools,
// allow the rest).
func WithPermissionPolicy(p permissions.Policy) HarnessOption {
	return func(o *harnessOptions) { o.permissionPolicy = &p }
}

// WithPermissionsDisabled skips the permissions extension entirely — every
// tool call runs unquestioned. Explicit opt-out for headless or fully
// trusted drivers.
func WithPermissionsDisabled() HarnessOption {
	return func(o *harnessOptions) { o.permissionsDisabled = true }
}

// WithApprovalTimeout bounds how long an AskUser tool call waits for an
// approval response before being denied. Default 120s; 0 denies immediately
// (unattended drivers with nobody to answer).
func WithApprovalTimeout(d time.Duration) HarnessOption {
	return func(o *harnessOptions) { o.approvalTimeout = d }
}

// WithBudgets registers the budgets extension for the main loop: the turn
// terminates gracefully (TurnResult.Terminated, surfaced by RunTurn as a
// "terminated: ..." error) when any limit is exceeded. Zero fields are
// unlimited.
func WithBudgets(l budgets.Limits) HarnessOption {
	return func(o *harnessOptions) { o.budgetLimits = l }
}

// WithMCPServer mounts an external MCP server (stdio transport) as a dynamic
// tool source: its tools appear as "mcp__<server>__<tool>" and are re-listed
// at each turn boundary. MCP-origin tools require approval under the default
// permission policy. Repeat the option per server.
func WithMCPServer(cfg mcp.ServerConfig) HarnessOption {
	return func(o *harnessOptions) { o.mcpServers = append(o.mcpServers, cfg) }
}
