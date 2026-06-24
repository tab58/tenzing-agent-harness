package tooldef

import (
	"context"
	"encoding/json"
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
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("file_path and content are required", WithError()), nil
	}

	var input struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.FilePath == "" {
		return NewToolResult("file_path is required", WithError()), nil
	}

	filePath := input.FilePath
	content := input.Content

	if existing, err := os.ReadFile(filePath); err == nil {
		t.snapshots.Save(filePath, existing)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NewToolResult(fmt.Sprintf("cannot create directory %q: %v", dir, err), WithError()), nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return NewToolResult(fmt.Sprintf("cannot write file: %v", err), WithError()), nil
	}

	return NewToolResult("File written: " + filePath), nil
}
