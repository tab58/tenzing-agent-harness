package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
)

var _ Definition = (*SubLMTool)(nil)

const defaultSubLMMaxTokens int64 = 4096

type SubLMTool struct {
	queryFn func(ctx context.Context, prompt string, maxTokens int64) (string, error)
}

func NewSubLMTool(queryFn func(ctx context.Context, prompt string, maxTokens int64) (string, error)) *SubLMTool {
	return &SubLMTool{queryFn: queryFn}
}

func (t *SubLMTool) Name() string { return "sub_lm" }

func (t *SubLMTool) Description() string {
	return "Make a single LLM query and return the response. " +
		"Use for processing chunks of text, answering sub-questions, " +
		"or decomposing complex analysis into smaller pieces. " +
		"The sub-LLM has no access to tools — for tool-using subtasks, use SubagentSpawn instead. " +
		"Prefer sub_lm when you need a simple answer to a focused question about a piece of text."
}

func (t *SubLMTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"prompt":     {Type: JsonTypeString},
			"max_tokens": {Type: JsonTypeNumber},
		},
		Required: []string{"prompt"},
	}
}

func (t *SubLMTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) < 1 || exctx.Arguments[0] == "" {
		return NewToolResult("prompt is required", WithError()), nil
	}

	var input struct {
		Prompt    string `json:"prompt"`
		MaxTokens int64  `json:"max_tokens"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.Prompt == "" {
		return NewToolResult("prompt is required", WithError()), nil
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = defaultSubLMMaxTokens
	}

	result, err := t.queryFn(ctx, input.Prompt, input.MaxTokens)
	if err != nil {
		return NewToolResult(fmt.Sprintf("sub_lm error: %v", err), WithError()), nil
	}

	return NewToolResult(result), nil
}
