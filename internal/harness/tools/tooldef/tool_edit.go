package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

func (t *EditTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("file_path, old_string, and new_string are required", WithError()), nil
	}

	var input struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.FilePath == "" || input.OldString == "" {
		return NewToolResult("file_path and old_string are required", WithError()), nil
	}

	filePath := input.FilePath
	oldString := input.OldString
	newString := input.NewString
	replaceAll := input.ReplaceAll

	data, err := os.ReadFile(filePath)
	if err != nil {
		return NewToolResult(fmt.Sprintf("cannot read file: %v", err), WithError()), nil
	}

	content := string(data)
	count := strings.Count(content, oldString)

	if !replaceAll {
		switch count {
		case 0:
			return NewToolResult("old_string not found", WithError()), nil
		case 1:
		default:
			return NewToolResult(fmt.Sprintf("old_string not unique: %d occurrences", count), WithError()), nil
		}
	} else if count == 0 {
		return NewToolResult("old_string not found", WithError()), nil
	}

	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
	} else {
		updated = strings.Replace(content, oldString, newString, 1)
	}

	if err := os.WriteFile(filePath, []byte(updated), 0644); err != nil {
		return NewToolResult(fmt.Sprintf("cannot write file: %v", err), WithError()), nil
	}

	return NewToolResult("Edit applied."), nil
}
