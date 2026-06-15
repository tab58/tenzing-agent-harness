package tooldef

import (
	"context"
	"encoding/json"
	"tenzing-agent/internal/harness"
)

type Definition interface {
	Name() string
	Description() string
	Schema() Schema
	Execute(ctx context.Context, exctx ExecutionContext) (harness.ToolResult, error)
}

type ExecutionContext struct {
	Arguments  []string
	WorkingDir string
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
	JsonTypeObject = "object"
	JsonTypeString = "string"
	JsonTypeNumber  = "number"
	JsonTypeBoolean = "boolean"
)

type SchemaProperty struct {
	Type JsonType `json:"type"`
}
