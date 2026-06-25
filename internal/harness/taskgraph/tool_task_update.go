package taskgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TaskUpdateTool)(nil)

type TaskUpdater interface {
	UpdateTask(taskID string, status string, result string) error
}

type TaskUpdateTool struct {
	updater TaskUpdater
}

func NewTaskUpdateTool(updater TaskUpdater) *TaskUpdateTool {
	return &TaskUpdateTool{updater: updater}
}

func (t *TaskUpdateTool) Name() string { return "task_update" }

func (t *TaskUpdateTool) Description() string {
	return "Update a task's status. Valid statuses: pending, in_progress, done, blocked. Optionally include a result summary. Accepts full ID or prefix."
}

func (t *TaskUpdateTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"task_id": {Type: tooldef.JsonTypeString},
			"status":  {Type: tooldef.JsonTypeString},
			"result":  {Type: tooldef.JsonTypeString},
		},
		Required: []string{"task_id", "status"},
	}
}

func (t *TaskUpdateTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	var args struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if len(exctx.Arguments) == 0 {
		return tooldef.NewToolResult("missing arguments", tooldef.WithError()), nil
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &args); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid arguments: %v", err), tooldef.WithError()), nil
	}
	if args.TaskID == "" || args.Status == "" {
		return tooldef.NewToolResult("task_id and status are required", tooldef.WithError()), nil
	}

	if err := t.updater.UpdateTask(args.TaskID, args.Status, args.Result); err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}
	return tooldef.NewToolResult(fmt.Sprintf("task %s → %s", args.TaskID, args.Status)), nil
}
