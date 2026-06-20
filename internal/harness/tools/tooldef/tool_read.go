package tooldef

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultReadLimit = 2000
)

var _ Definition = (*ReadTool)(nil)

type ReadTool struct{}

func (t *ReadTool) Name() string { return "Read" }

func (t *ReadTool) Description() string {
	return "Read a file and return its contents with line numbers."
}

func (t *ReadTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"file_path": {Type: JsonTypeString},
			"limit":     {Type: JsonTypeNumber},
			"offset":    {Type: JsonTypeNumber},
		},
		Required: []string{"file_path"},
	}
}

func (t *ReadTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	args := exctx.Arguments
	if len(args) == 0 || args[0] == "" {
		return ToolResult{Output: "file_path is required", IsError: true}, nil
	}
	filePath := args[0]

	offset := 0
	limit := defaultReadLimit
	if len(args) > 1 && args[1] != "" {
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 0 {
			return ToolResult{Output: "limit must be a non-negative integer", IsError: true}, nil
		}
		limit = n
	}
	if len(args) > 2 && args[2] != "" {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			return ToolResult{Output: "offset must be a non-negative integer", IsError: true}, nil
		}
		offset = n
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("cannot read file: %v", err), IsError: true}, nil
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if offset > len(lines) {
		offset = len(lines)
	}
	lines = lines[offset:]
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}

	var sb strings.Builder
	for i, line := range lines {
		lineNum := offset + i + 1
		fmt.Fprintf(&sb, "%6d\t%s\n", lineNum, line)
	}

	return ToolResult{Output: sb.String()}, nil
}
