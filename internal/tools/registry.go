package tools

import (
	"context"
	"fmt"
	"os"
	"tenzing-agent/internal/tools/tooldef"
)

type Registry struct {
	tools map[string]tooldef.Definition
	cwd   string
}

func NewRegistry(cwd string) (*Registry, error) {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("unable to determine working directory: %w", err)
		}
		cwd = wd
	}
	return &Registry{
		tools: make(map[string]tooldef.Definition),
		cwd:   cwd,
	}, nil
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
	resolved := r.resolveWorkingDir(exctx)
	result, err := toolDef.Execute(ctx, resolved)
	if err != nil {
		return tooldef.ToolResult{}, fmt.Errorf("error executing tool %s: %w", name, err)
	}
	return result, nil
}

func (r *Registry) resolveWorkingDir(exctx tooldef.ExecutionContext) tooldef.ExecutionContext {
	if exctx.WorkingDir != "" {
		return exctx
	}
	return tooldef.ExecutionContext{
		Arguments:  exctx.Arguments,
		WorkingDir: r.cwd,
	}
}
