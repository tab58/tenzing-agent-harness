package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*SnapshotWriteTool)(nil)

type SnapshotWriteTool struct {
	snapshots *Store
}

func NewWriteTool(snapshots *Store) *SnapshotWriteTool {
	return &SnapshotWriteTool{snapshots: snapshots}
}

func (t *SnapshotWriteTool) Name() string { return "Write" }

func (t *SnapshotWriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed. Automatically snapshots the previous content so it can be reverted."
}

func (t *SnapshotWriteTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"file_path": {Type: tooldef.JsonTypeString},
			"content":   {Type: tooldef.JsonTypeString},
		},
		Required: []string{"file_path", "content"},
	}
}

func (t *SnapshotWriteTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return tooldef.NewToolResult("file_path and content are required", tooldef.WithError()), nil
	}

	var input struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), tooldef.WithError()), nil
	}
	if input.FilePath == "" {
		return tooldef.NewToolResult("file_path is required", tooldef.WithError()), nil
	}

	filePath := input.FilePath
	content := input.Content

	if existing, err := os.ReadFile(filePath); err == nil {
		t.snapshots.Save(filePath, existing)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("cannot create directory %q: %v", dir, err), tooldef.WithError()), nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("cannot write file: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult("File written: " + filePath), nil
}
