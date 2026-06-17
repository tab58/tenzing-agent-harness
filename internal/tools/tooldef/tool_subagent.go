package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
)

var _ Definition = (*SubagentTool)(nil)

type SubagentTool struct {
	spawnFn func(ctx context.Context, prompt string) (string, error)
}

func NewSubagentTool(spawnFn func(ctx context.Context, prompt string) (string, error)) *SubagentTool {
	return &SubagentTool{spawnFn: spawnFn}
}

func (t *SubagentTool) Name() string { return "SubagentSpawn" }

func (t *SubagentTool) Description() string {
	return "Spawn a subagent to handle a subtask independently. " +
		"Use for exploration, high-output-volume tasks, risky operations, " +
		"or tasks that would pollute your context with intermediate details. " +
		"Input: a detailed prompt describing the subtask. " +
		"The subagent runs with its own context and tools. " +
		"Returns only the subagent's final summary."
}

func (t *SubagentTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"prompt": {Type: JsonTypeString},
		},
		Required: []string{"prompt"},
	}
}

func (t *SubagentTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) < 1 || exctx.Arguments[0] == "" {
		return ToolResult{Output: "prompt is required", IsError: true}, nil
	}

	var input struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return ToolResult{Output: fmt.Sprintf("invalid input JSON: %v", err), IsError: true}, nil
	}
	if input.Prompt == "" {
		return ToolResult{Output: "prompt is required", IsError: true}, nil
	}

	result, err := t.spawnFn(ctx, input.Prompt)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("subagent error: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: result}, nil
}
