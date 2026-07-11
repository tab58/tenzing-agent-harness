package harness

import (
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"

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

	// onTextDelta is called with incremental text output from the agent.
	// It is called from the agent's goroutine, so it should not block.
	onTextDelta func(string)

	// onThinkingDelta is called with incremental thinking output from the
	// agent. Called from the agent's goroutine, so it should not block.
	onThinkingDelta func(string)

	// eventBus is an event bus that transports all events
	eventBus *events.EventBus

	// mainSystemPrompt is the system prompt of the main agent
	mainSystemPrompt string

	// hooks holds optional typed callbacks dispatched from the event
	// bus. Only set hooks fire; leave the rest nil.
	hooks events.Hooks

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
}

func defaultHarnessOptions() *harnessOptions {
	return &harnessOptions{
		eventBus: events.NewEventBus(),
		skillDirs: []string{
			"~/.claude/skills",
		},
		extraTools:            make(map[string]tooldef.Definition),
		baseURLs:              make(map[common.Provider]string),
		subagentMaxDepth:      1,
		subagentMaxIterations: 100,
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

// WithBlackboardModel sets the model used for llm_query/llm_batch sub-LM
// calls inside the shared blackboard REPL; unset falls back to the main
// model.
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

func WithHooks(hooks events.Hooks) HarnessOption {
	return func(o *harnessOptions) {
		o.hooks = hooks
	}
}

func WithSystemPrompt(prompt string) HarnessOption {
	return func(o *harnessOptions) {
		o.mainSystemPrompt = prompt
	}
}

func WithEventBus(bus *events.EventBus) HarnessOption {
	return func(o *harnessOptions) {
		o.eventBus = bus
	}
}

func WithTextDeltaHandler(f func(string)) HarnessOption {
	return func(o *harnessOptions) {
		o.onTextDelta = f
	}
}

func WithThinkingDeltaHandler(f func(string)) HarnessOption {
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
