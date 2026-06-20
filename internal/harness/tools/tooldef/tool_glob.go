package tooldef

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var _ Definition = (*GlobTool)(nil)

type GlobTool struct{}

func (t *GlobTool) Name() string { return "Glob" }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern relative to the working directory."
}

func (t *GlobTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"pattern": {Type: JsonTypeString},
		},
		Required: []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	args := exctx.Arguments
	if len(args) == 0 || args[0] == "" {
		return ToolResult{Output: "pattern is required", IsError: true}, nil
	}
	pattern := args[0]

	if !filepath.IsAbs(pattern) && exctx.WorkingDir != "" {
		pattern = filepath.Join(exctx.WorkingDir, pattern)
	}

	root := globRoot(pattern)

	var (
		matches []string
		err     error
	)
	if strings.Contains(pattern, "**") {
		matches, err = globDoublestar(root, pattern)
	} else {
		matches, err = filepath.Glob(pattern)
	}
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("invalid glob pattern: %v", err), IsError: true}, nil
	}

	if len(matches) == 0 {
		return ToolResult{Output: "No matches."}, nil
	}
	return ToolResult{Output: strings.Join(matches, "\n")}, nil
}

func globRoot(pattern string) string {
	i := strings.IndexAny(pattern, "*?[")
	if i == -1 {
		return pattern
	}
	return filepath.Dir(pattern[:i])
}

func globDoublestar(root, pattern string) ([]string, error) {
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return nil, err
	}

	var matches []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if re.MatchString(path) {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, nil
}

func globToRegexp(pattern string) string {
	var sb strings.Builder
	sb.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				sb.WriteString(".*")
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					i++
				}
			} else {
				sb.WriteString("[^/]*")
				i++
			}
		case '?':
			sb.WriteString("[^/]")
			i++
		case '.', '+', '(', ')', '{', '}', '[', ']', '^', '$', '|', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(pattern[i])
			i++
		default:
			sb.WriteByte(pattern[i])
			i++
		}
	}
	sb.WriteString("$")
	return sb.String()
}
