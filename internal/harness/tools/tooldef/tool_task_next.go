package tooldef

import "context"

var _ Definition = (*TaskNextTool)(nil)

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

func (t *TaskNextTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TaskNextTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	result, err := t.nexter.NextTask()
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if result == "" {
		return ToolResult{Output: "(no unblocked tasks)"}, nil
	}
	return ToolResult{Output: result}, nil
}
