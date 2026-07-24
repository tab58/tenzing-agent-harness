package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/adapters/toolport"
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/features/prompts"
)

// AgentRunner is a thin facade over core.Loop. It owns construction-time
// wiring (tool definitions, skill map, stream callbacks) and translates
// TurnResult into the (string, error) contract the harness expects.
type AgentRunner struct {
	id           string
	agent        core.Agent
	loop         *core.Loop
	systemPrompt string
}

type agentRunnerOptions struct {
	id              string
	toolRegistry    *toolport.Registry
	toolPort        core.ToolPort
	contextStore    core.ContextPort
	onTextDelta     func(string)
	onThinkingDelta func(string)
	systemPrompt    string
	emitter         core.Emitter
	extensions      *core.Extensions
	approvalTimeout time.Duration
}

type AgentRunnerOption func(*agentRunnerOptions)

func WithEmitter(emitter core.Emitter) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.emitter = emitter
	}
}

// WithID assigns a pre-generated runner ID (see NewID) instead of a random
// one. Used when the ID must be known before the runner exists — e.g.
// hierarchical sub-agent IDs that double as blackboard slot names.
func WithID(id string) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		if id != "" {
			o.id = id
		}
	}
}

func WithTextDeltaHandler(f func(string)) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.onTextDelta = f
	}
}

func WithThinkingDeltaHandler(f func(string)) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.onThinkingDelta = f
	}
}

func WithSystemPrompt(prompt string) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		if prompt != "" {
			o.systemPrompt = prompt
		}
	}
}

func WithToolRegistry(registry *toolport.Registry) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.toolRegistry = registry
	}
}

// WithToolPort supplies the ToolPort the loop drives (e.g. the composite).
// When unset, the runner wraps the tool registry into a plain port.
func WithToolPort(port core.ToolPort) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.toolPort = port
	}
}

// WithContextStore configures the ContextPort that owns this runner's
// conversation history. Required — the runner rebuilds messages from it
// every iteration and appends the assistant/tool_result turns back to it.
func WithContextStore(cs core.ContextPort) AgentRunnerOption {
	return func(o *agentRunnerOptions) {
		o.contextStore = cs
	}
}

func WithExtensions(exts *core.Extensions) AgentRunnerOption {
	return func(o *agentRunnerOptions) { o.extensions = exts }
}

// WithApprovalTimeout bounds how long an AskUser tool call blocks awaiting
// an ApprovalRequestedEvent response. 0 (the default) denies immediately.
func WithApprovalTimeout(d time.Duration) AgentRunnerOption {
	return func(o *agentRunnerOptions) { o.approvalTimeout = d }
}

// NewAgentRunner creates a new AgentRunner. It performs one-time wiring
// (tool definitions, skill map, stream callbacks on the Agent) and builds
// a core.Loop for RunLoop to delegate to.
func NewAgentRunner(agent core.Agent, opts ...AgentRunnerOption) (*AgentRunner, error) {
	if agent == nil {
		return nil, fmt.Errorf("no agent defined")
	}

	o := &agentRunnerOptions{
		systemPrompt: prompts.DefaultSystemPrompt(),
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.toolPort == nil && o.toolRegistry == nil {
		return nil, fmt.Errorf("no tool registry defined")
	}
	if o.contextStore == nil {
		return nil, fmt.Errorf("no context store defined")
	}
	if o.id == "" {
		o.id = NewID()
	}
	if o.extensions == nil {
		o.extensions = core.NewExtensions()
	}

	// One-time agent wiring — these are construction-time setups, not
	// per-iteration calls. The core.Loop never touches them.
	if o.onTextDelta != nil {
		agent.UpdateStreamCallback(o.onTextDelta)
	}
	if o.onThinkingDelta != nil {
		agent.UpdateThinkingCallback(o.onThinkingDelta)
	}

	tp := o.toolPort
	if tp == nil {
		tp = toolport.Wrap(o.toolRegistry)
	}

	loop, err := core.NewLoop(core.LoopConfig{
		ID:              o.id,
		Model:           agent,
		Tools:           tp,
		Context:         o.contextStore,
		Emitter:         o.emitter,
		Extensions:      o.extensions,
		SystemPrompt:    o.systemPrompt,
		ApprovalTimeout: o.approvalTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create loop: %w", err)
	}

	return &AgentRunner{
		id:           o.id,
		agent:        agent,
		loop:         loop,
		systemPrompt: o.systemPrompt,
	}, nil
}

// NewID returns a fresh 8-hex-char runner ID (e.g. "438314ea"). Exported so
// callers can pre-generate an ID and pass it via WithID.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *AgentRunner) GetCurrentModel() string {
	return h.agent.GetCurrentModel()
}

// RunLoop executes a single turn: user input -> agent plan/execute loop -> agent result.
// It delegates to core.Loop.RunTurn and translates TurnResult into the
// (string, error) contract the harness expects.
func (h *AgentRunner) RunLoop(ctx context.Context, input string) (string, error) {
	tr, err := h.loop.RunTurn(ctx, input)
	if err != nil {
		return "", err
	}
	if tr.Terminated != "" {
		return tr.FinalAnswer, fmt.Errorf("terminated: %s", tr.Terminated)
	}
	return tr.FinalAnswer, nil
}

func (h *AgentRunner) ID() string {
	return h.id
}

func (h *AgentRunner) SystemPrompt() string {
	return h.systemPrompt
}
