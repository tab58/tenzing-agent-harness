package tooldef

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxGrepMatches = 500
)

var _ Definition = (*GrepTool)(nil)

type GrepTool struct{}

func (t *GrepTool) Name() string { return "Grep" }

func (t *GrepTool) Description() string {
	return "Search files for a regexp pattern, returning file:line:content matches."
}

func (t *GrepTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"pattern": {Type: JsonTypeString},
			"path":    {Type: JsonTypeString},
			"include": {Type: JsonTypeString},
		},
		Required: []string{"pattern"},
	}
}

func (t *GrepTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("pattern is required", WithError()), nil
	}

	var input struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.Pattern == "" {
		return NewToolResult("pattern is required", WithError()), nil
	}

	re, err := regexp.Compile(input.Pattern)
	if err != nil {
		return NewToolResult(fmt.Sprintf("invalid regexp: %v", err), WithError()), nil
	}

	searchRoot := exctx.WorkingDir
	if input.Path != "" {
		searchRoot = input.Path
	}
	if searchRoot == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return ToolResult{}, fmt.Errorf("unable to get cwd: %w", wdErr)
		}
		searchRoot = wd
	}

	includePattern := input.Include

	var matches []string
	capped := false
	err = filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		if includePattern != "" {
			matched, matchErr := filepath.Match(includePattern, filepath.Base(path))
			if matchErr != nil || !matched {
				return nil
			}
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if isBinary(data) {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", path, i+1, line))
				if len(matches) >= maxGrepMatches {
					capped = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return NewToolResult(fmt.Sprintf("walk error: %v", err), WithError()), nil
	}

	if len(matches) == 0 {
		return NewToolResult("No matches."), nil
	}
	output := strings.Join(matches, "\n")
	if capped {
		output += fmt.Sprintf("\n[truncated at %d matches]", maxGrepMatches)
	}
	return NewToolResult(output), nil
}

func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 1024 {
		probe = probe[:1024]
	}
	return bytes.IndexByte(probe, 0) != -1
}
