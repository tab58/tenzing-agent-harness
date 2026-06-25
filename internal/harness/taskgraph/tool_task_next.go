package taskgraph

import (
	"context"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*TaskNextTool)(nil)

type TaskNexter interface {
	NextTask() (string, error)
}

type TaskNextTool struct {
	nexter TaskNexter
}

func NewTaskNextTool(nexter TaskNexter) *TaskNextTool {
	return &TaskNextTool{nexter: nexter}
}

func (t *TaskNextTool) Name() string { return "task_next" }

func (t *TaskNextTool) Description() string {
	return "Get the next available task. Returns the highest-priority pending task whose dependencies are all done. Empty if nothing is unblocked."
}

func (t *TaskNextTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TaskNextTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	result, err := t.nexter.NextTask()
	if err != nil {
		return tooldef.NewToolResult(err.Error(), tooldef.WithError()), nil
	}
	if result == "" {
		return tooldef.NewToolResult("(no unblocked tasks)"), nil
	}
	return tooldef.NewToolResult(result), nil
}
