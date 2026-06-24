package tooldef

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

var _ Definition = (*RevertTool)(nil)

type RevertTool struct {
	snapshots *SnapshotStore
}

func NewRevertTool(snapshots *SnapshotStore) *RevertTool {
	return &RevertTool{snapshots: snapshots}
}

func (t *RevertTool) Name() string { return "Revert" }

func (t *RevertTool) Description() string {
	return "Restore a file to its state before the last write call. Use when a write introduced an error and you need to undo it."
}

func (t *RevertTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"file_path": {Type: JsonTypeString},
		},
		Required: []string{"file_path"},
	}
}

func (t *RevertTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("file_path is required", WithError()), nil
	}

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.FilePath == "" {
		return NewToolResult("file_path is required", WithError()), nil
	}

	filePath := input.FilePath

	content, ok := t.snapshots.Pop(filePath)
	if !ok {
		return NewToolResult(fmt.Sprintf("no snapshot available for %s", filePath), WithError()), nil
	}

	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return NewToolResult(fmt.Sprintf("cannot revert file: %v", err), WithError()), nil
	}

	return NewToolResult("File reverted: " + filePath), nil
}
