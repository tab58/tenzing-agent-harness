package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
)

var _ Definition = (*TaskUpdateTool)(nil)

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

func (t *TaskUpdateTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"task_id": {Type: JsonTypeString},
			"status":  {Type: JsonTypeString},
			"result":  {Type: JsonTypeString},
		},
		Required: []string{"task_id", "status"},
	}
}

func (t *TaskUpdateTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	var args struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if len(exctx.Arguments) == 0 {
		return NewToolResult("missing arguments", WithError()), nil
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &args); err != nil {
		return NewToolResult(fmt.Sprintf("invalid arguments: %v", err), WithError()), nil
	}
	if args.TaskID == "" || args.Status == "" {
		return NewToolResult("task_id and status are required", WithError()), nil
	}

	if err := t.updater.UpdateTask(args.TaskID, args.Status, args.Result); err != nil {
		return NewToolResult(err.Error(), WithError()), nil
	}
	return NewToolResult(fmt.Sprintf("task %s → %s", args.TaskID, args.Status)), nil
}
