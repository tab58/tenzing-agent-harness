package tooldef

import (
	"context"
	"encoding/json"
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
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("file_path is required", WithError()), nil
	}

	var input struct {
		FilePath string `json:"file_path"`
		Limit    *int   `json:"limit"`
		Offset   *int   `json:"offset"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.FilePath == "" {
		return NewToolResult("file_path is required", WithError()), nil
	}

	filePath := input.FilePath
	offset := 0
	limit := defaultReadLimit
	if input.Limit != nil {
		if *input.Limit < 0 {
			return NewToolResult("limit must be a non-negative integer", WithError()), nil
		}
		limit = *input.Limit
	}
	if input.Offset != nil {
		if *input.Offset < 0 {
			return NewToolResult("offset must be a non-negative integer", WithError()), nil
		}
		offset = *input.Offset
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return NewToolResult(fmt.Sprintf("cannot read file: %v", err), WithError()), nil
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	if offset > totalLines {
		offset = totalLines
	}
	visible := lines[offset:]
	truncated := false
	if limit > 0 && len(visible) > limit {
		visible = visible[:limit]
		truncated = true
	}

	var sb strings.Builder
	for i, line := range visible {
		lineNum := offset + i + 1
		fmt.Fprintf(&sb, "%6d\t%s\n", lineNum, line)
	}

	endLine := offset + len(visible)
	if truncated {
		fmt.Fprintf(&sb, "\n[Showing lines %d-%d of %d. For full-file analysis use rlm; to page use Read with offset=%d.]",
			offset+1, endLine, totalLines, endLine)
	} else {
		fmt.Fprintf(&sb, "\n[%d lines]", totalLines)
	}

	return NewToolResult(sb.String(), WithMetadata(map[string]string{
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
		"total":  strconv.Itoa(totalLines),
		"fp":     filePath,
	})), nil
}
