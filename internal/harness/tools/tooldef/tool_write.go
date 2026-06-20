package tooldef

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

var _ Definition = (*WriteTool)(nil)

type WriteTool struct {
	snapshots *SnapshotStore
}

func NewWriteTool(snapshots *SnapshotStore) *WriteTool {
	return &WriteTool{snapshots: snapshots}
}

func (t *WriteTool) Name() string { return "Write" }

func (t *WriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed. Automatically snapshots the previous content so it can be reverted."
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

func (t *WriteTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	args := exctx.Arguments
	if len(args) < 2 {
		return ToolResult{Output: "file_path and content are required", IsError: true}, nil
	}
	filePath := args[0]
	content := args[1]

	if filePath == "" {
		return ToolResult{Output: "file_path is required", IsError: true}, nil
	}

	if existing, err := os.ReadFile(filePath); err == nil {
		t.snapshots.Save(filePath, existing)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ToolResult{Output: fmt.Sprintf("cannot create directory %q: %v", dir, err), IsError: true}, nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return ToolResult{Output: fmt.Sprintf("cannot write file: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: "File written: " + filePath}, nil
}
