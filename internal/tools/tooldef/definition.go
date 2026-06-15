package tooldef

import (
	"context"
	"encoding/json"
)

type ToolResult struct {
	ToolUseID string
	Output    string
	IsError   bool
}

type ToolCall struct {
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
)

type SchemaProperty struct {
	Type JsonType `json:"type"`
}
