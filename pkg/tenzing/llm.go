package tenzing

import (
	provider "github.com/tab58/llm-providers"
	"github.com/tab58/llm-providers/common"
)

// LLM is the provider-agnostic client interface. Implement it (or build one
// with LLMFromModel/LLMFromEnv) and inject via WithLLMFactory.
type LLM = common.LLM

// Request/response types used by every LLM implementation.
type (
	CompletionRequest  = common.CompletionRequest
	CompletionResponse = common.CompletionResponse
	ContentBlock       = common.ContentBlock
	Message            = common.Message
	Role               = common.Role
	Usage              = common.Usage
	TokenCount         = common.TokenCount
	ModelInfo          = common.ModelInfo
)

// LLMToolDefinition is the provider-layer tool schema passed to
// LLM.SendMessageWithTools. Distinct from ToolDefinition, which is the
// harness-side tool contract (tooldef.Definition).
type LLMToolDefinition = common.ToolDefinition

// Message roles.
const (
	RoleUser      = common.RoleUser
	RoleAssistant = common.RoleAssistant
	RoleSystem    = common.RoleSystem
	RoleTool      = common.RoleTool
)

// Content block types (ContentBlock.Type values).
type ContentType = common.ContentType

const (
	ContentTypeText       = common.ContentTypeText
	ContentTypeToolUse    = common.ContentTypeToolUse
	ContentTypeToolResult = common.ContentTypeToolResult
)

// Content and message constructors, plus text-block helpers.
var (
	NewTextContent       = common.NewTextContent
	NewToolUseContent    = common.NewToolUseContent
	NewToolResultContent = common.NewToolResultContent
	NewUserMessage       = common.NewUserMessage
	NewAssistantMessage  = common.NewAssistantMessage
	NewSystemMessage     = common.NewSystemMessage
	CombinedText         = common.CombinedText
)

// Sentinel errors, for errors.Is checks without importing llm-providers.
var (
	ErrNotSupported    = common.ErrNotSupported
	ErrUnknownProvider = common.ErrUnknownProvider
)

// Streaming.
type (
	StreamEvent     = common.StreamEvent
	StreamEventType = common.StreamEventType
)

const (
	StreamEventStart    = common.StreamEventStart
	StreamEventDelta    = common.StreamEventDelta
	StreamEventThinking = common.StreamEventThinking
	StreamEventStop     = common.StreamEventStop
	StreamEventError    = common.StreamEventError
)

// Stop reasons.
type StopReason = common.StopReason

const (
	StopReasonEndTurn   = common.StopReasonEndTurn
	StopReasonMaxTokens = common.StopReasonMaxTokens
	StopReasonToolUse   = common.StopReasonToolUse
	StopReasonStop      = common.StopReasonStop
)

// Client constructors and their options, from the llm-providers root package.
// LLMFromModel takes an explicit API key (e.g. sourced from config);
// LLMFromEnv resolves the key from the provider's conventional env var.
type ClientOption = provider.Option

var (
	LLMFromModel = provider.LLMFromModel
	LLMFromEnv   = provider.LLMFromEnv
	WithBaseURL  = provider.WithBaseURL
)
