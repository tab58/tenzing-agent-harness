package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
	agentctx "tenzing-agent/internal/agent/context"
)

var _ Definition = (*TaskCreateTool)(nil)

type TaskCreator interface {
	CreateTask(desc string, dependsOn []string, priority agentctx.TaskPriority) (string, error)
}

type TaskCreateTool struct {
	creator TaskCreator
}

func NewTaskCreateTool(creator TaskCreator) *TaskCreateTool {
	return &TaskCreateTool{creator: creator}
}

func (t *TaskCreateTool) Name() string { return "task_create" }

func (t *TaskCreateTool) Description() string {
	return "Create a task in the persistent task graph. Supports dependencies on other task IDs and priority (high, medium, low). Use for multi-step work that must survive restarts."
}

func (t *TaskCreateTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"description": {Type: JsonTypeString},
			"depends_on":  {Type: JsonTypeArray, Items: &SchemaProperty{Type: JsonTypeString}},
			"priority":    {Type: JsonTypeString},
		},
		Required: []string{"description"},
	}
}

func (t *TaskCreateTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	var args struct {
		Description string                `json:"description"`
		DependsOn   []string              `json:"depends_on"`
		Priority    agentctx.TaskPriority `json:"priority"`
	}
	if len(exctx.Arguments) == 0 {
		return ToolResult{Output: "missing arguments", IsError: true}, nil
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &args); err != nil {
		return ToolResult{Output: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}
	if args.Description == "" {
		return ToolResult{Output: "description is required", IsError: true}, nil
	}

	result, err := t.creator.CreateTask(args.Description, args.DependsOn, args.Priority)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}
