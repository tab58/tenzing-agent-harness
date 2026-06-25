package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

type ToolProvider interface {
	GetTools() []tooldef.Definition
}

type Registry struct {
	tools map[string]tooldef.Definition
	// workingDir string
}

// func NewRegistry(workingDir string) *Registry {
func NewRegistry() *Registry {
	r := &Registry{
		tools: make(map[string]tooldef.Definition),
		// workingDir: workingDir,
	}

	// basic tools
	builtins := []tooldef.Definition{
		&tooldef.BashTool{},
		&tooldef.ReadTool{},
		&tooldef.EditTool{},
		&tooldef.GrepTool{},
		&tooldef.GlobTool{},
	}

	for _, def := range builtins {
		r.Register(def)
	}

	return r
}

func (r *Registry) Register(def tooldef.Definition) error {
	name := strings.ToLower(def.Name())
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool already registered")
	}
	r.tools[name] = def
	return nil
}

func (r *Registry) RegisterFromProvider(provider ToolProvider) error {
	tools := provider.GetTools()
	for _, def := range tools {
		if err := r.Register(def); err != nil {
			return fmt.Errorf("unable to register tool: %w", err)
		}
	}
	return nil
}

// CopyWithout copies the tools in the Registry without certain ones
func (r *Registry) CopyWithout(names ...string) *Registry {
	exclude := make(map[string]struct{}, len(names))
	for _, n := range names {
		exclude[strings.ToLower(n)] = struct{}{}
	}
	filtered := NewRegistry()
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

func (r *Registry) ProviderDefinitions() []provider.ToolDefinition {
	defs := r.Definitions()
	providerDefs := make([]provider.ToolDefinition, len(defs))
	for i, d := range defs {
		schema, _ := json.Marshal(d.Schema())
		providerDefs[i] = provider.ToolDefinition{
			Name:        d.Name(),
			Description: d.Description(),
			InputSchema: schema,
		}
	}
	return providerDefs
}

// func (r *Registry) WorkingDir() string {
// 	return r.workingDir
// }

func (r *Registry) Execute(ctx context.Context, name string, input string) (tooldef.ToolResult, error) {
	toolDef, ok := r.tools[strings.ToLower(name)]
	if !ok {
		available := make([]string, 0, len(r.tools))
		for n := range r.tools {
			available = append(available, n)
		}
		slog.Warn("unknown tool called", "tool", name, "available", available)
		return tooldef.ToolResult{
			Output:  fmt.Sprintf("Tool %q not found. Available tools: %s", name, strings.Join(available, ", ")),
			IsError: true,
		}, nil
	}
	exctx := tooldef.ExecutionContext{
		Arguments: []string{input},
		// WorkingDir: r.workingDir,
	}
	result, err := toolDef.Execute(ctx, exctx)
	if err != nil {
		return tooldef.ToolResult{}, fmt.Errorf("error executing tool %s: %w", name, err)
	}
	return result, nil
}
