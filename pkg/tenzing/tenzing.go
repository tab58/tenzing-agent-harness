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
	"github.com/tab58/tenzing-agent-harness/internal/adapters/eventbus"
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/core/tooldef"
	"github.com/tab58/tenzing-agent-harness/internal/features/budgets"
	"github.com/tab58/tenzing-agent-harness/internal/features/mcp"
	"github.com/tab58/tenzing-agent-harness/internal/features/permissions"
	"github.com/tab58/tenzing-agent-harness/internal/harness"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
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
	WithAgentBuilder            = harness.WithAgentBuilder
	WithLLMFactory              = harness.WithLLMFactory
	WithProviderBaseURL         = harness.WithProviderBaseURL
	WithSubagentModel           = harness.WithSubagentModel
	WithSubagentDepth           = harness.WithSubagentDepth
	WithSubagentMaxIterations   = harness.WithSubagentMaxIterations
	WithBlackboardModel         = harness.WithBlackboardModel
	WithAdvisorModel            = harness.WithAdvisorModel
	WithDisabledTool            = harness.WithDisabledTool
	WithSkillsDir               = harness.WithSkillsDir
	WithTool                    = harness.WithTool
	WithHooks                   = harness.WithHooks
	WithSystemPrompt            = harness.WithSystemPrompt
	WithConversationID          = harness.WithConversationID
	WithEventBus                = harness.WithEventBus
	WithTextDeltaHandler        = harness.WithTextDeltaHandler
	WithThinkingDeltaHandler    = harness.WithThinkingDeltaHandler
	WithExtension               = harness.WithExtension
	WithPermissionPolicy        = harness.WithPermissionPolicy
	WithPermissionsDisabled     = harness.WithPermissionsDisabled
	WithApprovalTimeout         = harness.WithApprovalTimeout
	WithSessionDir              = harness.WithSessionDir
	WithSessionDisabled         = harness.WithSessionDisabled
	WithPromptTemplatesDir      = harness.WithPromptTemplatesDir
	WithContextFilesDisabled    = harness.WithContextFilesDisabled
	WithToolCallGate            = harness.WithToolCallGate
	WithThinking                = harness.WithThinking
	WithLLMRetry                = harness.WithLLMRetry
	WithCompressionThreshold    = harness.WithCompressionThreshold
	WithCompressionKeepMessages = harness.WithCompressionKeepMessages
)

// ToolCallGate is the pre-execution veto installed via WithToolCallGate.
type ToolCallGate = harness.ToolCallGate

// ReadOnlyReporter is the optional marker interface a custom tool
// implements (ReadOnly() bool → true) to run concurrently with adjacent
// read-only calls in a tool batch; unmarked tools act as barriers.
type ReadOnlyReporter = tooldef.ReadOnlyReporter

// ErrVisionUnsupported is returned by Harness.RunTurnWithImages when the
// current model does not accept image input (checked before any API call).
var ErrVisionUnsupported = harness.ErrVisionUnsupported

// Tool permission policy (default-on: code-executing/file-writing tools ask
// for approval; opt out with WithPermissionsDisabled).
type PermissionPolicy = permissions.Policy

var DefaultPermissionPolicy = permissions.DefaultPolicy

// Budget limits for WithBudgets; zero fields are unlimited.
type BudgetLimits = budgets.Limits

var WithBudgets = harness.WithBudgets

// MCP server config for WithMCPServer (stdio transport).
type MCPServerConfig = mcp.ServerConfig

var WithMCPServer = harness.WithMCPServer

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
	Agent           = core.Agent
	AgentBuilder    = runner.AgentBuilder
	ReasoningResult = core.ReasoningResult
)

// Tool types, for implementing custom tools passed via WithTool.
type (
	ToolDefinition   = tooldef.Definition
	ToolResult       = core.ToolResult
	ToolResultOption = tooldef.ToolResultOption
	ToolCall         = core.ToolCall
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
	Event    = core.Event
	EventBus = eventbus.EventBus
	Hooks    = eventbus.Hooks
)

var NewEventBus = eventbus.NewEventBus

// Typed events delivered to Hooks callbacks and EventBus subscribers.
type (
	SessionStartedEvent        = core.SessionStartedEvent
	SessionEndedEvent          = core.SessionEndedEvent
	TurnStartedEvent           = core.TurnStartedEvent
	TurnCompletedEvent         = core.TurnCompletedEvent
	LoopStartedEvent           = core.LoopStartedEvent
	LoopStoppedEvent           = core.LoopStoppedEvent
	ReasoningStartedEvent      = core.ReasoningStartedEvent
	ReasoningFinishedEvent     = core.ReasoningFinishedEvent
	ToolExecutionStartedEvent  = core.ToolExecutionStartedEvent
	ToolExecutionFinishedEvent = core.ToolExecutionFinishedEvent
	LLMResponseEvent           = core.LLMResponseEvent
	ToolSucceededEvent         = core.ToolSucceededEvent
	ToolFailedEvent            = core.ToolFailedEvent
	ToolDeniedEvent            = core.ToolDeniedEvent
	ToolProgressEvent          = core.ToolProgressEvent
	ContextCompressingEvent    = core.ContextCompressingEvent
	ContextCompressedEvent     = core.ContextCompressedEvent
	ErrorEvent                 = core.ErrorEvent
	SubagentStartedEvent       = core.SubagentStartedEvent
	SubagentStoppedEvent       = core.SubagentStoppedEvent
	TaskCreatedEvent           = core.TaskCreatedEvent
	TaskCompletedEvent         = core.TaskCompletedEvent
	ApprovalRequestedEvent     = core.ApprovalRequestedEvent
	SteeringInjectedEvent      = core.SteeringInjectedEvent
	LLMRetryEvent              = core.LLMRetryEvent
	ModelChangedEvent          = core.ModelChangedEvent
	ThinkingChangedEvent       = core.ThinkingChangedEvent
	ImagesAttachedEvent        = core.ImagesAttachedEvent
	ImageData                  = core.ImageData
)
