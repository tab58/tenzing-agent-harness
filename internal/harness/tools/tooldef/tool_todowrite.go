package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
)

type TodoItem struct {
	Index  int    `json:"index"`
	Task   string `json:"task"`
	Status string `json:"status"`
}

var _ Definition = (*TodoWriteTool)(nil)

type TodoWriteTool struct{}

func NewTodoWriteTool() *TodoWriteTool {
	return &TodoWriteTool{}
}

func (t *TodoWriteTool) Name() string { return "TodoWrite" }

func (t *TodoWriteTool) Description() string {
	return "Write a plan of tasks before executing. ALWAYS call this tool first before starting any multi-step work. Input: a JSON array of task description strings."
}

func (t *TodoWriteTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"tasks": {
				Type:  JsonTypeArray,
				Items: &SchemaProperty{Type: JsonTypeString},
			},
		},
		Required: []string{"tasks"},
	}
}

func (t *TodoWriteTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	args := exctx.Arguments
	if len(args) < 1 {
		return NewToolResult("tasks argument is required", WithError()), nil
	}
	var tasks []string
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &tasks); err != nil {
		return NewToolResult(fmt.Sprintf("invalid tasks JSON: %v", err), WithError()), nil
	}
	if len(tasks) == 0 {
		return NewToolResult("tasks list cannot be empty", WithError()), nil
	}

	items := make([]TodoItem, len(tasks))
	for i, task := range tasks {
		items[i] = TodoItem{
			Index:  i,
			Task:   task,
			Status: "pending",
		}
	}
	if err := writeTodoItems(exctx.WorkingDir, items); err != nil {
		return NewToolResult(fmt.Sprintf("failed to write todo file: %v", err), WithError()), nil
	}

	return NewToolResult(fmt.Sprintf("Plan written: %d tasks", len(items))), nil
}
