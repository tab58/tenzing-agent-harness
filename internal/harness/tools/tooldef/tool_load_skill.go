package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
)

var _ Definition = (*LoadSkillTool)(nil)

type SkillContentLoader interface {
	Load(name string) (string, error)
}

type LoadSkillTool struct {
	loader SkillContentLoader
}

func NewLoadSkillTool(loader SkillContentLoader) *LoadSkillTool {
	return &LoadSkillTool{loader: loader}
}

func (t *LoadSkillTool) Name() string { return "load_skill" }

func (t *LoadSkillTool) Description() string {
	return "Load full instructions for a named skill. Call list_skills first to see available skills. Do NOT guess skill names."
}

func (t *LoadSkillTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"name": {Type: JsonTypeString},
		},
		Required: []string{"name"},
	}
}

func (t *LoadSkillTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if len(exctx.Arguments) == 0 {
		return NewToolResult("missing arguments", WithError()), nil
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &args); err != nil {
		return NewToolResult(fmt.Sprintf("invalid arguments: %v", err), WithError()), nil
	}
	content, err := t.loader.Load(args.Name)
	if err != nil {
		return NewToolResult(err.Error(), WithError()), nil
	}
	return NewToolResult(content), nil
}
