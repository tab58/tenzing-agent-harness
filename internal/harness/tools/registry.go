package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

func GetDefaultToolDefs(skillRegistry *skills.Registry) []tooldef.Definition {
	snapshotStore := tooldef.NewSnapshotStore()
	return []tooldef.Definition{
		// basic tools
		&tooldef.BashTool{},
		&tooldef.ReadTool{},
		&tooldef.EditTool{},
		&tooldef.GrepTool{},
		&tooldef.GlobTool{},

		// subagent spawn
		&tooldef.SubagentTool{},

		// snapshot-required
		tooldef.NewWriteTool(snapshotStore),
		tooldef.NewRevertTool(snapshotStore),

		// todo planner tools
		tooldef.NewTodoReadTool(),
		tooldef.NewTodoWriteTool(),
		tooldef.NewTodoUpdateTool(),

		// skills tools
		tooldef.NewLoadSkillTool(skillRegistry),
		tooldef.NewListSkillsTool(skillRegistry),
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
