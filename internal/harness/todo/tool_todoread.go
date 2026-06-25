package todo

import (
	"context"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TodoReadTool)(nil)

type TodoReadTool struct {
	file *TodoFile
}

func NewTodoReadTool(f *TodoFile) *TodoReadTool {
	return &TodoReadTool{file: f}
}

func (t *TodoReadTool) Name() string { return "TodoRead" }

func (t *TodoReadTool) Description() string {
	return "Read the current plan and check progress. Returns all tasks with their current status."
}

func (t *TodoReadTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TodoReadTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	items, err := t.file.ReadItems()
	if err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}
	return tooldef.NewToolResult(t.file.FormatItems(items)), nil
}
