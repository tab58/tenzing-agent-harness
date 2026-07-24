package tooldef

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tab58/llm-providers/common"

	"github.com/tab58/tenzing-agent-harness/internal/core"
)

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

func NewToolResult(output string, options ...ToolResultOption) core.ToolResult {
	o := &toolResultOptions{}
	for _, option := range options {
		option(o)
	}

	return core.ToolResult{
		Output:    output,
		ToolUseID: o.ToolUseID,
		IsError:   o.IsError,
		Metadata:  o.Metadata,
	}
}

type Definition interface {
	Name() string
	Description() string
	Schema() Schema
	Execute(ctx context.Context, exctx ExecutionContext) (core.ToolResult, error)
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
	JsonTypeInteger = "integer"
	JsonTypeBoolean = "boolean"
	JsonTypeArray   = "array"
)

type SchemaProperty struct {
	Type  JsonType        `json:"type"`
	Items *SchemaProperty `json:"items,omitempty"`
}

// SpecFromDefinition wraps a tooldef.Definition into an origin-tagged
// core.ToolSpec so extensions can reuse existing tool implementations
// without registry registration.
func SpecFromDefinition(def Definition, origin string) core.ToolSpec {
	schema, _ := json.Marshal(def.Schema())
	return core.ToolSpec{
		Definition: common.ToolDefinition{
			Name:        def.Name(),
			Description: def.Description(),
			InputSchema: schema,
		},
		Origin: origin,
		Execute: func(ctx context.Context, call core.ToolCall) core.ToolResult {
			res, err := def.Execute(ctx, ExecutionContext{Arguments: []string{call.Input}})
			if err != nil {
				return core.ToolResult{ToolUseID: call.ID, Output: fmt.Sprintf("tool execution failed: %v", err), IsError: true}
			}
			res.ToolUseID = call.ID
			return res
		},
	}
}
