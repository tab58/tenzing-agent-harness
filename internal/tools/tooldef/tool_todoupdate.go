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
		return ToolResult{Output: "index and status arguments are required", IsError: true}, nil
	}

	index, err := strconv.Atoi(exctx.Arguments[0])
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("invalid index: %v", err), IsError: true}, nil
	}

	status := exctx.Arguments[1]
	if status == "" {
		return ToolResult{Output: "status cannot be empty", IsError: true}, nil
	}

	items, err := readTodoItems(exctx.WorkingDir)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	if index < 0 || index >= len(items) {
		return ToolResult{Output: fmt.Sprintf("index %d out of range (0-%d)", index, len(items)-1), IsError: true}, nil
	}

	updated := make([]TodoItem, len(items))
	copy(updated, items)
	updated[index] = TodoItem{
		Index:  items[index].Index,
		Task:   items[index].Task,
		Status: status,
	}

	if err := writeTodoItems(exctx.WorkingDir, updated); err != nil {
		return ToolResult{Output: fmt.Sprintf("failed to update todo: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: fmt.Sprintf("Task %d updated to %s\n\n%s", index, status, formatTodoItems(updated))}, nil
}
