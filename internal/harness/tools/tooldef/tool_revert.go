package tooldef

import (
	"context"
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
	args := exctx.Arguments
	if len(args) < 1 {
		return ToolResult{Output: "file_path is required", IsError: true}, nil
	}
	filePath := args[0]

	if filePath == "" {
		return ToolResult{Output: "file_path is required", IsError: true}, nil
	}

	content, ok := t.snapshots.Pop(filePath)
	if !ok {
		return ToolResult{Output: fmt.Sprintf("no snapshot available for %s", filePath), IsError: true}, nil
	}

	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return ToolResult{Output: fmt.Sprintf("cannot revert file: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: "File reverted: " + filePath}, nil
}
