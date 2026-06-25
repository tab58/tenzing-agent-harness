package taskgraph

import (
	"context"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TaskListTool)(nil)

type TaskLister interface {
	ListTasks() (string, error)
}

type TaskListTool struct {
	lister TaskLister
}

func NewTaskListTool(lister TaskLister) *TaskListTool {
	return &TaskListTool{lister: lister}
}

func (t *TaskListTool) Name() string { return "task_list" }

func (t *TaskListTool) Description() string {
	return "List all tasks in the task graph with their IDs, statuses, priorities, and dependencies."
}

func (t *TaskListTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TaskListTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	result, err := t.lister.ListTasks()
	if err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}
	if result == "[]" {
		return tooldef.NewToolResult("(no tasks)"), nil
	}
	return tooldef.NewToolResult(result), nil
}
