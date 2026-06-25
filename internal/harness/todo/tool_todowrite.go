package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TodoWriteTool)(nil)

type TodoWriteTool struct {
	file *TodoFile
}

func NewTodoWriteTool(f *TodoFile) *TodoWriteTool {
	return &TodoWriteTool{file: f}
}

func (t *TodoWriteTool) Name() string { return "TodoWrite" }

func (t *TodoWriteTool) Description() string {
	return "Write a plan of tasks before executing. ALWAYS call this tool first before starting any multi-step work. Input: a JSON array of task description strings."
}

func (t *TodoWriteTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"tasks": {
				Type:  tooldef.JsonTypeArray,
				Items: &tooldef.SchemaProperty{Type: tooldef.JsonTypeString},
			},
		},
		Required: []string{"tasks"},
	}
}

func (t *TodoWriteTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	args := exctx.Arguments
	if len(args) < 1 {
		return tooldef.NewToolResult("tasks argument is required", tooldef.WithError()), nil
	}
	var tasks []string
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &tasks); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid tasks JSON: %v", err), tooldef.WithError()), nil
	}
	if len(tasks) == 0 {
		return tooldef.NewToolResult("tasks list cannot be empty", tooldef.WithError()), nil
	}

	items := make([]TodoItem, len(tasks))
	for i, task := range tasks {
		items[i] = TodoItem{
			Index:  i,
			Task:   task,
			Status: "pending",
		}
	}
	if err := t.file.WriteItems(items); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("failed to write todo file: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult(fmt.Sprintf("Plan written: %d tasks", len(items))), nil
}
