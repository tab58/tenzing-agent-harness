// Package tenzing is the public API for running agents programmatically.
// It re-exports the harness constructor, options, and the supporting types
// needed to use them from outside this module. Create a harness with New,
// then run a single loop with Harness.RunTurn.
//
//	h, err := tenzing.New(model, tenzing.WithSystemPrompt("..."))
//	if err != nil { ... }
//	defer h.Shutdown()
//	answer, err := h.RunTurn(ctx, "do the thing")
package tenzing

import (
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/harness"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"
)

// Harness wires an agent, tool registry, and event bus into a runnable loop.
type Harness = harness.Harness

// Option configures a Harness at construction time.
type Option = harness.HarnessOption

// New constructs a Harness for the given main model. See the With* options
// for configuration.
var New = harness.New

// Harness options.
var (
	WithAgentBuilder          = harness.WithAgentBuilder
	WithLLMFactory            = harness.WithLLMFactory
	WithProviderBaseURL       = harness.WithProviderBaseURL
	WithSubagentModel         = harness.WithSubagentModel
	WithSubagentDepth         = harness.WithSubagentDepth
	WithSubagentMaxIterations = harness.WithSubagentMaxIterations
	WithBlackboardModel       = harness.WithBlackboardModel
	WithAdvisorModel          = harness.WithAdvisorModel
	WithDisabledTool          = harness.WithDisabledTool
	WithSkillsDir             = harness.WithSkillsDir
	WithTool                  = harness.WithTool
	WithHooks                 = harness.WithHooks
	WithSystemPrompt          = harness.WithSystemPrompt
	WithConversationID        = harness.WithConversationID
	WithEventBus              = harness.WithEventBus
	WithTextDeltaHandler      = harness.WithTextDeltaHandler
	WithThinkingDeltaHandler  = harness.WithThinkingDeltaHandler
	WithBlackboardDisabled    = harness.WithBlackboardDisabled
	WithExtension             = harness.WithExtension
)

// Extension system: implement Extension plus any of the capability hook
// interfaces below and register via WithExtension.
type (
	Extension = core.Extension

	SessionStartHook    = core.SessionStartHook
	SessionEndHook      = core.SessionEndHook
	BeforeIterationHook = core.BeforeIterationHook
	ToolCallHook        = core.ToolCallHook
	ToolResultHook      = core.ToolResultHook
	AfterTurnHook       = core.AfterTurnHook
	PromptContributor   = core.PromptContributor
	ToolProvider        = core.ToolProvider
	DynamicToolSource   = core.DynamicToolSource

	TurnContext       = core.TurnContext
	ToolCallContext   = core.ToolCallContext
	ToolResultContext = core.ToolResultContext
	TurnResult        = core.TurnResult
	ToolSpec          = core.ToolSpec
	Decision          = core.Decision
)

// Tool-gating decisions for ToolCallHook implementations.
const (
	Allow   = core.Allow
	AskUser = core.AskUser
	Deny    = core.Deny
)

// Agent is the "brain" contract consumed by the runner; implement it and
// pass a builder via WithAgentBuilder to replace the default agent.
type (
	Agent           = runner.Agent
	AgentBuilder    = runner.AgentBuilder
	ReasoningResult = runner.ReasoningResult
)

// Tool types, for implementing custom tools passed via WithTool.
type (
	ToolDefinition   = tooldef.Definition
	ToolResult       = tooldef.ToolResult
	ToolResultOption = tooldef.ToolResultOption
	ToolCall         = tooldef.ToolCall
	ExecutionContext = tooldef.ExecutionContext
	Schema           = tooldef.Schema
	SchemaProperty   = tooldef.SchemaProperty
	JsonType         = tooldef.JsonType
)

// Tool result constructors.
var (
	NewToolResult       = tooldef.NewToolResult
	WithToolUseID       = tooldef.WithToolUseID
	WithToolMetadata    = tooldef.WithMetadata
	WithToolResultError = tooldef.WithError
)

// JSON schema type names for tool schemas.
const (
	JsonTypeObject  = tooldef.JsonTypeObject
	JsonTypeString  = tooldef.JsonTypeString
	JsonTypeNumber  = tooldef.JsonTypeNumber
	JsonTypeInteger = tooldef.JsonTypeInteger
	JsonTypeBoolean = tooldef.JsonTypeBoolean
	JsonTypeArray   = tooldef.JsonTypeArray
)

// Event system, for WithEventBus / WithHooks consumers.
type (
	Event    = events.Event
	EventBus = events.EventBus
	Hooks    = events.Hooks
)

var NewEventBus = events.NewEventBus

// Typed events delivered to Hooks callbacks and EventBus subscribers.
type (
	SessionStartedEvent        = events.SessionStartedEvent
	SessionEndedEvent          = events.SessionEndedEvent
	TurnStartedEvent           = events.TurnStartedEvent
	TurnCompletedEvent         = events.TurnCompletedEvent
	LoopStartedEvent           = events.LoopStartedEvent
	LoopStoppedEvent           = events.LoopStoppedEvent
	ReasoningStartedEvent      = events.ReasoningStartedEvent
	ReasoningFinishedEvent     = events.ReasoningFinishedEvent
	ToolExecutionStartedEvent  = events.ToolExecutionStartedEvent
	ToolExecutionFinishedEvent = events.ToolExecutionFinishedEvent
	LLMResponseEvent           = events.LLMResponseEvent
	ToolSucceededEvent         = events.ToolSucceededEvent
	ToolFailedEvent            = events.ToolFailedEvent
	ToolProgressEvent          = events.ToolProgressEvent
	ContextCompressingEvent    = events.ContextCompressingEvent
	ContextCompressedEvent     = events.ContextCompressedEvent
	ErrorEvent                 = events.ErrorEvent
	SubagentStartedEvent       = events.SubagentStartedEvent
	SubagentStoppedEvent       = events.SubagentStoppedEvent
	TaskCreatedEvent           = events.TaskCreatedEvent
	TaskCompletedEvent         = events.TaskCompletedEvent
)
