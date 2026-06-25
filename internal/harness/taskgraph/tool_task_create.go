package taskgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TaskCreateTool)(nil)

type TaskCreator interface {
	CreateTask(desc string, dependsOn []string, priority TaskPriority) (string, error)
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

func (t *TaskCreateTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"description": {Type: tooldef.JsonTypeString},
			"depends_on":  {Type: tooldef.JsonTypeArray, Items: &tooldef.SchemaProperty{Type: tooldef.JsonTypeString}},
			"priority":    {Type: tooldef.JsonTypeString},
		},
		Required: []string{"description"},
	}
}

func (t *TaskCreateTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	var args struct {
		Description string       `json:"description"`
		DependsOn   []string     `json:"depends_on"`
		Priority    TaskPriority `json:"priority"`
	}
	if len(exctx.Arguments) == 0 {
		return tooldef.NewToolResult("missing arguments", tooldef.WithError()), nil
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &args); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid arguments: %v", err), tooldef.WithError()), nil
	}
	if args.Description == "" {
		return tooldef.NewToolResult("description is required", tooldef.WithError()), nil
	}

	result, err := t.creator.CreateTask(args.Description, args.DependsOn, args.Priority)
	if err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}
	return tooldef.NewToolResult(result), nil
}
