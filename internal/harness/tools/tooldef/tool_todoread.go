package tooldef

import "context"

var _ Definition = (*TodoReadTool)(nil)

type TodoReadTool struct{}

func NewTodoReadTool() *TodoReadTool {
	return &TodoReadTool{}
}

func (t *TodoReadTool) Name() string { return "TodoRead" }

func (t *TodoReadTool) Description() string {
	return "Read the current plan and check progress. Returns all tasks with their current status."
}

func (t *TodoReadTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{},
		Required:   []string{},
	}
}

func (t *TodoReadTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	items, err := readTodoItems(exctx.WorkingDir)
	if err != nil {
		return NewToolResult(err.Error(), WithError()), nil
	}

	return NewToolResult(formatTodoItems(items)), nil
}
