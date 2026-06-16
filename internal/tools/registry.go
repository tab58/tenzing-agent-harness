package tools

import (
	"context"
	"fmt"
	"tenzing-agent/internal/tools/tooldef"
)

type Registry struct {
	tools map[string]tooldef.Definition
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]tooldef.Definition),
	}
}

func (r *Registry) Register(def tooldef.Definition) error {
	name := def.Name()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool already registered")
	}
	r.tools[name] = def
	return nil
}

func (r *Registry) Execute(ctx context.Context, name string, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	toolDef, ok := r.tools[name]
	if !ok {
		return tooldef.ToolResult{}, fmt.Errorf("tool name %s not found", name)
	}
	result, err := toolDef.Execute(ctx, exctx)
	if err != nil {
		return tooldef.ToolResult{}, fmt.Errorf("error executing tool %s: %w", name, err)
	}
	return result, nil
}

func NewDefaultRegistry() *Registry {
	r := NewRegistry()

	snapshots := tooldef.NewSnapshotStore()

	defaults := []tooldef.Definition{
		&tooldef.BashTool{},
		&tooldef.ReadTool{},
		tooldef.NewWriteTool(snapshots),
		&tooldef.EditTool{},
		&tooldef.GrepTool{},
		&tooldef.GlobTool{},
		tooldef.NewRevertTool(snapshots),
		tooldef.NewTodoWriteTool(),
		tooldef.NewTodoUpdateTool(),
		tooldef.NewTodoReadTool(),
	}

	for _, def := range defaults {
		r.Register(def)
	}

	return r
}
