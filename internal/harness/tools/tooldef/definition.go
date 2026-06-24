package tooldef

import (
	"context"
	"encoding/json"
)

type ToolResult struct {
	ToolUseID string
	Output    string
	IsError   bool
	Metadata  map[string]string
}

type toolResultOptions struct {
	ToolUseID string
	IsError   bool
	Metadata  map[string]string
}

type ToolResultOption func(*toolResultOptions)

func WithToolUseID(id string) ToolResultOption {
	return func(o *toolResultOptions) {
		o.ToolUseID = id
	}
}

func WithMetadata(metadata map[string]string) ToolResultOption {
	return func(o *toolResultOptions) {
		o.Metadata = metadata
	}
}

func WithError() ToolResultOption {
	return func(o *toolResultOptions) {
		o.IsError = true
	}
}

func NewToolResult(output string, options ...ToolResultOption) ToolResult {
	o := &toolResultOptions{}
	for _, option := range options {
		option(o)
	}

	return ToolResult{
		Output:    output,
		ToolUseID: o.ToolUseID,
		IsError:   o.IsError,
		Metadata:  o.Metadata,
	}
}

type ToolCall struct {
	ID    string
	Name  string
	Input string
}

type Definition interface {
	Name() string
	Description() string
	Schema() Schema
	Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error)
}

type ExecutionContext struct {
	Arguments  []string `json:"arguments"`
	WorkingDir string   `json:"working_dir"`
}

type Schema struct {
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

func (t Schema) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type       string                    `json:"type"`
		Properties map[string]SchemaProperty `json:"properties"`
		Required   []string                  `json:"required"`
	}{
		Type:       JsonTypeObject,
		Properties: t.Properties,
		Required:   t.Required,
	})
}

type JsonType string

func (t JsonType) String() string { return string(t) }

const (
	JsonTypeObject  = "object"
	JsonTypeString  = "string"
	JsonTypeNumber  = "number"
	JsonTypeBoolean = "boolean"
	JsonTypeArray   = "array"
)

type SchemaProperty struct {
	Type  JsonType        `json:"type"`
	Items *SchemaProperty `json:"items,omitempty"`
}
