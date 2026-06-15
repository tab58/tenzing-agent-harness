package tooldef

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"tenzing-agent/internal/harness"
)

var _ Definition = (*WriteTool)(nil)

type WriteTool struct{}

func (t *WriteTool) Name() string { return "Write" }

func (t *WriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed."
}

func (t *WriteTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"file_path": {Type: JsonTypeString},
			"content":   {Type: JsonTypeString},
		},
		Required: []string{"file_path", "content"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, exctx ExecutionContext) (harness.ToolResult, error) {
	args := exctx.Arguments
	if len(args) < 2 {
		return harness.ToolResult{Output: "file_path and content are required", IsError: true}, nil
	}
	filePath := args[0]
	content := args[1]

	if filePath == "" {
		return harness.ToolResult{Output: "file_path is required", IsError: true}, nil
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return harness.ToolResult{Output: fmt.Sprintf("cannot create directory %q: %v", dir, err), IsError: true}, nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return harness.ToolResult{Output: fmt.Sprintf("cannot write file: %v", err), IsError: true}, nil
	}

	return harness.ToolResult{Output: "File written: " + filePath}, nil
}
