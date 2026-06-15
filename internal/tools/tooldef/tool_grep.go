package tooldef

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"tenzing-agent/internal/harness"
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

func (t *GrepTool) Execute(ctx context.Context, exctx ExecutionContext) (harness.ToolResult, error) {
	args := exctx.Arguments
	if len(args) == 0 || args[0] == "" {
		return harness.ToolResult{Output: "pattern is required", IsError: true}, nil
	}
	pattern := args[0]

	re, err := regexp.Compile(pattern)
	if err != nil {
		return harness.ToolResult{Output: fmt.Sprintf("invalid regexp: %v", err), IsError: true}, nil
	}

	searchRoot := exctx.WorkingDir
	if len(args) > 1 && args[1] != "" {
		searchRoot = args[1]
	}
	if searchRoot == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return harness.ToolResult{}, fmt.Errorf("unable to get cwd: %w", wdErr)
		}
		searchRoot = wd
	}

	includePattern := ""
	if len(args) > 2 && args[2] != "" {
		includePattern = args[2]
	}

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
		return harness.ToolResult{Output: fmt.Sprintf("walk error: %v", err), IsError: true}, nil
	}

	if len(matches) == 0 {
		return harness.ToolResult{Output: "No matches."}, nil
	}
	output := strings.Join(matches, "\n")
	if capped {
		output += fmt.Sprintf("\n[truncated at %d matches]", maxGrepMatches)
	}
	return harness.ToolResult{Output: output}, nil
}

func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 1024 {
		probe = probe[:1024]
	}
	return bytes.IndexByte(probe, 0) != -1
}
