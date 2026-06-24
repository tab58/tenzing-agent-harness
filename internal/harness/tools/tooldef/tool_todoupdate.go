package tooldef

import (
	"context"
	"fmt"
	"strconv"
)

var _ Definition = (*TodoUpdateTool)(nil)

type TodoUpdateTool struct{}

func NewTodoUpdateTool() *TodoUpdateTool {
	return &TodoUpdateTool{}
}

func (t *TodoUpdateTool) Name() string { return "TodoUpdate" }

func (t *TodoUpdateTool) Description() string {
	return "Update the status of a single task in the current plan. Use this to mark tasks as in_progress, done, or blocked as you work through the plan."
}

func (t *TodoUpdateTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"index":  {Type: JsonTypeNumber},
			"status": {Type: JsonTypeString},
		},
		Required: []string{"index", "status"},
	}
}

func (t *TodoUpdateTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) < 2 {
		return NewToolResult("index and status arguments are required", WithError()), nil
	}

	index, err := strconv.Atoi(exctx.Arguments[0])
	if err != nil {
		return NewToolResult(fmt.Sprintf("invalid index: %v", err), WithError()), nil
	}

	status := exctx.Arguments[1]
	if status == "" {
		return NewToolResult("status cannot be empty", WithError()), nil
	}

	items, err := readTodoItems(exctx.WorkingDir)
	if err != nil {
		return NewToolResult(err.Error(), WithError()), nil
	}

	if index < 0 || index >= len(items) {
		return NewToolResult(fmt.Sprintf("index %d out of range (0-%d)", index, len(items)-1), WithError()), nil
	}

	updated := make([]TodoItem, len(items))
	copy(updated, items)
	updated[index] = TodoItem{
		Index:  items[index].Index,
		Task:   items[index].Task,
		Status: status,
	}

	if err := writeTodoItems(exctx.WorkingDir, updated); err != nil {
		return NewToolResult(fmt.Sprintf("failed to update todo: %v", err), WithError()), nil
	}

	return NewToolResult(fmt.Sprintf("Task %d updated to %s\n\n%s", index, status, formatTodoItems(updated))), nil
}
