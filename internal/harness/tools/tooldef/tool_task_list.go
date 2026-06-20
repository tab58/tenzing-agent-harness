package tooldef

import "context"

var _ Definition = (*TaskListTool)(nil)

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

func (t *TaskListTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TaskListTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	result, err := t.lister.ListTasks()
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if result == "[]" {
		return ToolResult{Output: "(no tasks)"}, nil
	}
	return ToolResult{Output: result}, nil
}
