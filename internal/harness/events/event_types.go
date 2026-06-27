package events

import "time"

// --- Session-level ---

type SessionStartedEvent struct {
	BaseEvent
}

type SessionEndedEvent struct {
	BaseEvent
	TurnCount int           `json:"turn_count"`
	Duration  time.Duration `json:"duration_ms"`
}

// --- Turn-level ---

type TurnStartedEvent struct {
	BaseEvent
	Query string `json:"query"`
}

type TurnCompletedEvent struct {
	BaseEvent
	FinalAnswer string        `json:"final_answer"`
	Iterations  int           `json:"iterations"`
	Duration    time.Duration `json:"duration_ms"`
}

// --- FSM-level ---

type LoopStartedEvent struct {
	BaseEvent
	Input string `json:"input"`
}

type LoopStoppedEvent struct {
	BaseEvent
	Iterations int           `json:"iterations"`
	Duration   time.Duration `json:"duration_ms"`
}

type ReasoningStartedEvent struct {
	BaseEvent
	Iteration int `json:"iteration"`
}

type ReasoningFinishedEvent struct {
	BaseEvent
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	StopReason   string `json:"stop_reason"`
	HasToolCall  bool   `json:"has_tool_call"`
}

type ToolExecutionStartedEvent struct {
	BaseEvent
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
}

type ToolExecutionFinishedEvent struct {
	BaseEvent
	ToolName string        `json:"tool_name"`
	Duration time.Duration `json:"duration_ms"`
}

// --- Business-level ---

type LLMResponseEvent struct {
	BaseEvent
	Model        string `json:"model"`
	ResponseID   string `json:"response_id"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	StopReason   string `json:"stop_reason"`
	Text         string `json:"text"`
}

type ToolSucceededEvent struct {
	BaseEvent
	ToolName string        `json:"tool_name"`
	Input    string        `json:"input"`
	Output   string        `json:"output"`
	Duration time.Duration `json:"duration_ms"`
}

type ToolFailedEvent struct {
	BaseEvent
	ToolName string        `json:"tool_name"`
	Input    string        `json:"input"`
	Error    string        `json:"error"`
	Duration time.Duration `json:"duration_ms"`
}

type ToolProgressEvent struct {
	BaseEvent
	ToolName  string `json:"tool_name"`
	Phase     string `json:"phase"`
	Detail    string `json:"detail"`
	Iteration int    `json:"iteration,omitempty"`
	TokensIn  int64  `json:"tokens_in,omitempty"`
	TokensOut int64  `json:"tokens_out,omitempty"`
}

type ContextCompressingEvent struct {
	BaseEvent
	MessageCount int `json:"message_count"`
}

type ContextCompressedEvent struct {
	BaseEvent
	MessagesBefore int    `json:"messages_before"`
	MessagesAfter  int    `json:"messages_after"`
	Summary        string `json:"summary"`
}

type ErrorEvent struct {
	BaseEvent
	Error   string `json:"error"`
	Context string `json:"context"`
}

// --- Subagent lifecycle ---

type SubagentStartedEvent struct {
	BaseEvent
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	Prompt    string `json:"prompt"`
}

type SubagentStoppedEvent struct {
	BaseEvent
	AgentID    string        `json:"agent_id"`
	AgentType  string        `json:"agent_type"`
	Iterations int           `json:"iterations"`
	Duration   time.Duration `json:"duration_ms"`
}

// --- Task lifecycle ---

type TaskCreatedEvent struct {
	BaseEvent
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
}

type TaskCompletedEvent struct {
	BaseEvent
	TaskID string `json:"task_id"`
}
