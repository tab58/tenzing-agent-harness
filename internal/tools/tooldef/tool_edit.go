package tooldef

import (
	"context"
	"fmt"
	"os"
	"strings"
	"tenzing-agent/internal/harness"
)

var _ Definition = (*EditTool)(nil)

type EditTool struct{}

func (t *EditTool) Name() string { return "Edit" }

func (t *EditTool) Description() string {
	return "Replace a string in a file. Fails if old_string is not found or is not unique (unless replace_all=true)."
}

func (t *EditTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"file_path":   {Type: JsonTypeString},
			"old_string":  {Type: JsonTypeString},
			"new_string":  {Type: JsonTypeString},
			"replace_all": {Type: JsonTypeBoolean},
		},
		Required: []string{"file_path", "old_string", "new_string"},
	}
}

func (t *EditTool) Execute(ctx context.Context, exctx ExecutionContext) (harness.ToolResult, error) {
	args := exctx.Arguments
	if len(args) < 3 {
		return harness.ToolResult{Output: "file_path, old_string, and new_string are required", IsError: true}, nil
	}
	filePath := args[0]
	oldString := args[1]
	newString := args[2]

	replaceAll := false
	if len(args) > 3 && args[3] == "true" {
		replaceAll = true
	}

	if filePath == "" {
		return harness.ToolResult{Output: "file_path is required", IsError: true}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return harness.ToolResult{Output: fmt.Sprintf("cannot read file: %v", err), IsError: true}, nil
	}

	content := string(data)
	count := strings.Count(content, oldString)

	if !replaceAll {
		switch count {
		case 0:
			return harness.ToolResult{Output: "old_string not found", IsError: true}, nil
		case 1:
		default:
			return harness.ToolResult{Output: fmt.Sprintf("old_string not unique: %d occurrences", count), IsError: true}, nil
		}
	} else if count == 0 {
		return harness.ToolResult{Output: "old_string not found", IsError: true}, nil
	}

	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
	} else {
		updated = strings.Replace(content, oldString, newString, 1)
	}

	if err := os.WriteFile(filePath, []byte(updated), 0644); err != nil {
		return harness.ToolResult{Output: fmt.Sprintf("cannot write file: %v", err), IsError: true}, nil
	}

	return harness.ToolResult{Output: "Edit applied."}, nil
}
