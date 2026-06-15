package tools

import (
	"context"
	"fmt"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/tools/tooldef"
)

type Registry struct {
	tools map[string]tooldef.Definition
}

func NewRegistry() (*Registry, error) {
	return &Registry{}, nil
}

func (r *Registry) Register(def tooldef.Definition) error {
	name := def.Name()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool already registered")
	}
	r.tools[name] = def
	return nil
}

func (r *Registry) Execute(ctx context.Context, name string, exctx tooldef.ExecutionContext) (harness.ToolResult, error) {
	toolDef, ok := r.tools[name]
	if !ok {
		return harness.ToolResult{}, fmt.Errorf("tool name %s not found", name)
	}
	result, err := toolDef.Execute(ctx, exctx)
	if err != nil {
		return harness.ToolResult{}, fmt.Errorf("error executing tool %s: %w", name, err)
	}
	return result, nil
}
