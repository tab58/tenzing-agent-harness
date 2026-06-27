package todo

import (
	"context"
	"encoding/json"
	"fmt"

	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TodoUpdateTool)(nil)

type TodoUpdateTool struct {
	file *TodoFile
}

func NewTodoUpdateTool(f *TodoFile) *TodoUpdateTool {
	return &TodoUpdateTool{file: f}
}

func (t *TodoUpdateTool) Name() string { return "TodoUpdate" }

func (t *TodoUpdateTool) Description() string {
	return "Update the status of a single task in the current plan. Use this to mark tasks as in_progress, done, or blocked as you work through the plan."
}

func (t *TodoUpdateTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"index":  {Type: tooldef.JsonTypeNumber},
			"status": {Type: tooldef.JsonTypeString},
		},
		Required: []string{"index", "status"},
	}
}

func (t *TodoUpdateTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return tooldef.NewToolResult("index and status arguments are required", tooldef.WithError()), nil
	}
	var input struct {
		Index  int    `json:"index"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), tooldef.WithError()), nil
	}
	index := input.Index
	status := input.Status
	if status == "" {
		return tooldef.NewToolResult("status cannot be empty", tooldef.WithError()), nil
	}

	items, err := t.file.ReadItems()
	if err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}

	if index < 0 || index >= len(items) {
		return tooldef.NewToolResult(fmt.Sprintf("index %d out of range (0-%d)", index, len(items)-1), tooldef.WithError()), nil
	}

	updated := make([]TodoItem, len(items))
	copy(updated, items)
	updated[index] = TodoItem{
		Index:  items[index].Index,
		Task:   items[index].Task,
		Status: status,
	}

	if err := t.file.WriteItems(updated); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("failed to update todo: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult(fmt.Sprintf("Task %d updated to %s\n\n%s", index, status, t.file.FormatItems(updated))), nil
}
