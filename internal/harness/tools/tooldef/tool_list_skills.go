package tooldef

import (
	"context"
	"fmt"
	"strings"
)

var _ Definition = (*ListSkillsTool)(nil)

type SkillLister interface {
	List() map[string]string
}

type ListSkillsTool struct {
	lister SkillLister
}

func NewListSkillsTool(lister SkillLister) *ListSkillsTool {
	return &ListSkillsTool{lister: lister}
}

func (t *ListSkillsTool) Name() string { return "list_skills" }

func (t *ListSkillsTool) Description() string {
	return "List all available skills. Call this to see what specialized knowledge is available before starting a task."
}

func (t *ListSkillsTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{},
		Required:   []string{},
	}
}

func (t *ListSkillsTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	skills := t.lister.List()
	if len(skills) == 0 {
		return ToolResult{Output: "No skills available."}, nil
	}
	var lines []string
	for name, desc := range skills {
		lines = append(lines, fmt.Sprintf("- %s: %s", name, desc))
	}
	return ToolResult{Output: strings.Join(lines, "\n")}, nil
}
