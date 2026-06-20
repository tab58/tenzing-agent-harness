package tooldef

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	bashTimeout = 120 * time.Second
)

var _ Definition = (*BashTool)(nil)

type BashTool struct{}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Execute a shell command in the project working directory."
}

func (t *BashTool) Schema() Schema {
	return Schema{
		Properties: map[string]SchemaProperty{
			"command":     {Type: JsonTypeString},
			"description": {Type: JsonTypeString},
		},
		Required: []string{"command"},
	}
}

func isValidDirectory(cwd string) bool {
	return true
}

func (t *BashTool) Execute(ctx context.Context, exctx ExecutionContext) (ToolResult, error) {
	args := exctx.Arguments
	if len(args) == 0 || args[0] == "" {
		return ToolResult{Output: "command is required", IsError: true}, nil
	}
	command := args[0]

	tctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()

	// set working directory
	cwd := exctx.WorkingDir
	if cwd == "" {
		// set as current working directory
		currentDir, err := os.Getwd()
		if err != nil {
			return ToolResult{}, fmt.Errorf("unable to get cwd: %w", err)
		}
		cwd = currentDir
	}

	// create bash command
	cmd := exec.CommandContext(tctx, "sh", "-c", command)
	if cwd != "" && isValidDirectory(cwd) {
		cmd.Dir = cwd
	}

	// output results
	out, execErr := cmd.CombinedOutput()
	output := string(out)
	if tctx.Err() == context.DeadlineExceeded {
		output += "\n[timed out]"
	}
	if execErr != nil {
		var exitErr *exec.ExitError
		if errors.As(execErr, &exitErr) {
			output = fmt.Sprintf("%s\n[exit status %d]", output, exitErr.ExitCode())
		} else {
			output = fmt.Sprintf("%s\nexec error: %v", output, execErr)
		}
		return ToolResult{Output: output, IsError: true}, nil
	}

	return ToolResult{Output: output}, nil
}
