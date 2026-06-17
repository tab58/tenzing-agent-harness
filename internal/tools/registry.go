package tools

import (
	"context"
	"fmt"
	"tenzing-agent/internal/tools/tooldef"
)

func GetDefaultToolDefs() []tooldef.Definition {
	snapshotStore := tooldef.NewSnapshotStore()
	return []tooldef.Definition{
		// basic tools
		&tooldef.BashTool{},
		&tooldef.ReadTool{},
		&tooldef.EditTool{},
		&tooldef.GrepTool{},
		&tooldef.GlobTool{},

		// snapshot-required
		tooldef.NewWriteTool(snapshotStore),
		tooldef.NewRevertTool(snapshotStore),

		// todo planner tools
		tooldef.NewTodoReadTool(),
		tooldef.NewTodoWriteTool(),
		tooldef.NewTodoUpdateTool(),
	}
}

type Registry struct {
	tools      map[string]tooldef.Definition
	workingDir string
}

func NewRegistry(workingDir string, tools ...tooldef.Definition) *Registry {
	r := &Registry{
		tools:      make(map[string]tooldef.Definition),
		workingDir: workingDir,
	}

	for _, def := range tools {
		r.Register(def)
	}

	return r
}

func (r *Registry) Register(def tooldef.Definition) error {
	name := def.Name()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool already registered")
	}
	r.tools[name] = def
	return nil
}

// CopyWithout copies the tools in the Registry without certain ones
func (r *Registry) CopyWithout(names ...string) *Registry {
	exclude := make(map[string]struct{}, len(names))
	for _, n := range names {
		exclude[n] = struct{}{}
	}
	filtered := NewRegistry(r.workingDir)
	for name, def := range r.tools {
		if _, skip := exclude[name]; !skip {
			filtered.tools[name] = def
		}
	}
	return filtered
}

func (r *Registry) Definitions() []tooldef.Definition {
	defs := make([]tooldef.Definition, 0, len(r.tools))
	for _, d := range r.tools {
		defs = append(defs, d)
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name string, input string) (tooldef.ToolResult, error) {
	toolDef, ok := r.tools[name]
	if !ok {
		return tooldef.ToolResult{}, fmt.Errorf("tool name %s not found", name)
	}
	exctx := tooldef.ExecutionContext{
		Arguments:  []string{input},
		WorkingDir: r.workingDir,
	}
	result, err := toolDef.Execute(ctx, exctx)
	if err != nil {
		return tooldef.ToolResult{}, fmt.Errorf("error executing tool %s: %w", name, err)
	}
	return result, nil
}
