package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"tenzing-agent/internal/harness/tools/tooldef"
)

var _ tooldef.Definition = (*SnapshotRevertTool)(nil)

type SnapshotRevertTool struct {
	snapshots *Store
}

func NewRevertTool(snapshots *Store) *SnapshotRevertTool {
	return &SnapshotRevertTool{snapshots: snapshots}
}

func (t *SnapshotRevertTool) Name() string { return "Revert" }

func (t *SnapshotRevertTool) Description() string {
	return "Restore a file to its state before the last write call. Use when a write introduced an error and you need to undo it."
}

func (t *SnapshotRevertTool) Schema() tooldef.Schema {
	return tooldef.Schema{
		Properties: map[string]tooldef.SchemaProperty{
			"file_path": {Type: tooldef.JsonTypeString},
		},
		Required: []string{"file_path"},
	}
}

func (t *SnapshotRevertTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return tooldef.NewToolResult("file_path is required", tooldef.WithError()), nil
	}

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), tooldef.WithError()), nil
	}
	if input.FilePath == "" {
		return tooldef.NewToolResult("file_path is required", tooldef.WithError()), nil
	}

	filePath := input.FilePath

	content, ok := t.snapshots.Pop(filePath)
	if !ok {
		return tooldef.NewToolResult(fmt.Sprintf("no snapshot available for %s", filePath), tooldef.WithError()), nil
	}

	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return tooldef.NewToolResult(fmt.Sprintf("cannot revert file: %v", err), tooldef.WithError()), nil
	}

	return tooldef.NewToolResult("File reverted: " + filePath), nil
}
