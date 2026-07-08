package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*SpawnAgentTool)(nil)

type AgentFactory interface {
	SpawnAgent(ctx context.Context, task string, context string) (string, error)
}

type SpawnAgentTool struct {
	factory AgentFactory
}

func NewSpawnAgentTool(factory AgentFactory) *SpawnAgentTool {
	return &SpawnAgentTool{factory: factory}
}

func (t *SpawnAgentTool) Name() string { return "spawn_agent" }

func (t *SpawnAgentTool) Description() string {
	return "Delegate a task to an autonomous sub-agent that runs its own " +
		"reasoning loop with full tool access (bash, read, edit, grep, glob, rlm). " +
		"Use for tasks requiring actions — editing files, running commands, investigating " +
		"failures. For analytical tasks over large inputs (summarization, extraction, " +
		"aggregation), prefer the rlm tool instead. The sub-agent runs to completion " +
		"and returns its final answer."
}

func (t *SpawnAgentTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"task":    {Type: tooldef.JsonTypeString},
			"context": {Type: tooldef.JsonTypeString},
		},
		Required: []string{"task"},
	}
}

func (t *SpawnAgentTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return tooldef.NewToolResult("task is required", tooldef.WithError()), nil
	}

	var input struct {
		Task    string `json:"task"`
		Context string `json:"context"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), tooldef.WithError()), nil
	}
	if input.Task == "" {
		return tooldef.NewToolResult("task is required", tooldef.WithError()), nil
	}

	result, err := t.factory.SpawnAgent(ctx, input.Task, input.Context)
	if err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("spawn_agent error: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult(result), nil
}
