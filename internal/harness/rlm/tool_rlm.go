package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*RLMTool)(nil)

type RLMTool struct {
	runFn func(ctx context.Context, prompt string) (string, error)
}

func NewRLMTool(runFn func(ctx context.Context, prompt string) (string, error)) *RLMTool {
	return &RLMTool{runFn: runFn}
}

func (t *RLMTool) Name() string { return "rlm" }

func (t *RLMTool) Description() string {
	return "Process a large input using a recursive language model with a Python REPL. " +
		"The input is loaded as a Python variable. You write Python code to programmatically " +
		"decompose, analyze (via sub_lm calls in loops), and aggregate results. " +
		"Use for inputs too large to process in a single pass, or when you need to run " +
		"sub-LLM queries in loops over chunks of text. Returns the final answer string."
}

func (t *RLMTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"prompt": {Type: tooldef.JsonTypeString},
		},
		Required: []string{"prompt"},
	}
}

func (t *RLMTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	if len(exctx.Arguments) < 1 || exctx.Arguments[0] == "" {
		return tooldef.NewToolResult("prompt is required", tooldef.WithError()), nil
	}

	var input struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), tooldef.WithError()), nil
	}
	if input.Prompt == "" {
		return tooldef.NewToolResult("prompt is required", tooldef.WithError()), nil
	}

	result, err := t.runFn(ctx, input.Prompt)
	if err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("rlm error: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult(result), nil
}
