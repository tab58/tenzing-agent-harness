package tooldef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
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
	if len(exctx.Arguments) == 0 || exctx.Arguments[0] == "" {
		return NewToolResult("command is required", WithError()), nil
	}

	var input struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(exctx.Arguments[0]), &input); err != nil {
		return NewToolResult(fmt.Sprintf("invalid input JSON: %v", err), WithError()), nil
	}
	if input.Command == "" {
		return NewToolResult("command is required", WithError()), nil
	}

	command := input.Command

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
	// On timeout, kill the whole process group: killing only the shell leaves
	// pipeline children holding the output pipe, blocking CombinedOutput long
	// past the deadline.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// Backstop: abandon the output pipe if something survives the group kill.
	cmd.WaitDelay = 5 * time.Second

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
		return NewToolResult(output, WithError()), nil
	}

	return NewToolResult(output), nil
}
