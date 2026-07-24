package blackboard

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const darwinSandboxProfile = `(version 1)` +
	`(deny default)` +
	`(allow file-read*)` +
	`(allow file-write-data)` +
	`(allow process-exec*)` +
	`(allow process-fork)` +
	`(allow sysctl-read)` +
	`(allow mach-lookup)` +
	`(allow signal)` +
	`(allow ipc-posix-shm-read*)`

//go:embed bootstrap.py
var bootstrapScript string

type REPL struct {
	cmd        *exec.Cmd
	stdin      *json.Encoder
	scanner    *bufio.Scanner
	workingDir string
	querier    Querier
}

type message struct {
	Type  string `json:"type"`
	Code  string `json:"code,omitempty"`
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`

	Stdout string `json:"stdout,omitempty"`
	Done   bool   `json:"done,omitempty"`
	Final  string `json:"final,omitempty"`
	Error  string `json:"error,omitempty"`

	Func  string         `json:"func,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	Event string         `json:"event,omitempty"`

	Result string `json:"result,omitempty"`
}

func newPythonCmd(script, workingDir string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err == nil {
			slog.Debug("[RLM] macOS sandbox-exec enabled")
			cmd := exec.Command("sandbox-exec", "-p", darwinSandboxProfile,
				"python3", "-u", "-B", "-c", script)
			cmd.Dir = workingDir
			return cmd
		}
		slog.Warn("[RLM] sandbox-exec not found, running without macOS sandbox")
	}
	cmd := exec.Command("python3", "-u", "-B", "-c", script)
	cmd.Dir = workingDir
	return cmd
}

func NewREPL(querier Querier, workingDir string) (*REPL, error) {
	cmd := newPythonCmd(bootstrapScript, workingDir)
	cmd.Stderr = os.Stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start python: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	repl := &REPL{
		cmd:        cmd,
		stdin:      json.NewEncoder(stdinPipe),
		scanner:    scanner,
		workingDir: workingDir,
		querier:    querier,
	}

	return repl, nil
}

func (r *REPL) Execute(ctx context.Context, code string) (stdout string, done bool, final string, err error) {
	if err := r.send(message{Type: "exec", Code: code}); err != nil {
		return "", false, "", fmt.Errorf("send exec: %w", err)
	}
	return r.readUntilResult(ctx)
}

func (r *REPL) SetVar(name string, value string) error {
	if err := r.send(message{Type: "set_var", Name: name, Value: value}); err != nil {
		return fmt.Errorf("send set_var: %w", err)
	}
	_, _, _, err := r.readUntilResult(context.Background())
	return err
}

func (r *REPL) Close() error {
	_ = r.send(message{Type: "shutdown"})
	return r.cmd.Wait()
}

func (r *REPL) send(msg message) error {
	return r.stdin.Encode(msg)
}

func (r *REPL) readUntilResult(ctx context.Context) (string, bool, string, error) {
	for {
		if ctx.Err() != nil {
			r.cmd.Process.Kill()
			return "", false, "", ctx.Err()
		}
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				r.cmd.Process.Kill()
				return "", false, "", fmt.Errorf("read from python: %w", err)
			}
			return "", false, "", fmt.Errorf("python process exited unexpectedly")
		}
		line := r.scanner.Text()

		var msg message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return "", false, "", fmt.Errorf("invalid JSON from python: %w", err)
		}

		switch msg.Type {
		case "result":
			return msg.Stdout, msg.Done, msg.Final, nil

		case "debug":
			slog.Debug("[RLM:py] "+msg.Event, "data", line)

		case "callback":
			result, cbErr := r.handleCallback(ctx, msg.Func, msg.Args)
			resp := message{Type: "callback_result", Result: result}
			if cbErr != nil {
				resp.Error = cbErr.Error()
			}
			if err := r.send(resp); err != nil {
				return "", false, "", fmt.Errorf("send callback_result: %w", err)
			}

		default:
			return "", false, "", fmt.Errorf("unexpected message type: %s", msg.Type)
		}
	}
}

func (r *REPL) handleCallback(ctx context.Context, funcName string, args map[string]any) (string, error) {
	switch funcName {
	case "sub_lm":
		return r.callbackSubLM(ctx, args)
	case "sub_lm_batch":
		return r.callbackSubLMBatch(ctx, args)
	case "read_file":
		return r.callbackReadFile(args)
	case "grep_file":
		return r.callbackGrepFile(args)
	case "list_files":
		return r.callbackListFiles(args)
	default:
		return "", fmt.Errorf("unknown callback: %s", funcName)
	}
}

func (r *REPL) callbackSubLM(ctx context.Context, args map[string]any) (string, error) {
	if r.querier == nil {
		return "", fmt.Errorf("sub_lm not available: no querier configured")
	}
	prompt, _ := args["prompt"].(string)
	maxTokens := int64(4096)
	if mt, ok := args["max_tokens"].(float64); ok {
		maxTokens = int64(mt)
	}
	return r.querier.Query(ctx, prompt, maxTokens)
}

func (r *REPL) callbackSubLMBatch(ctx context.Context, args map[string]any) (string, error) {
	if r.querier == nil {
		return "", fmt.Errorf("sub_lm_batch not available: no querier configured")
	}
	promptsRaw, _ := args["prompts"].([]any)
	maxTokens := int64(4096)
	if mt, ok := args["max_tokens"].(float64); ok {
		maxTokens = int64(mt)
	}

	prompts := make([]string, 0, len(promptsRaw))
	for _, p := range promptsRaw {
		if s, ok := p.(string); ok && s != "" {
			prompts = append(prompts, s)
		}
	}
	if len(prompts) == 0 {
		return "[]", nil
	}

	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	results := make([]string, len(prompts))
	errs := make([]error, len(prompts))
	var wg sync.WaitGroup

	for i, p := range prompts {
		wg.Add(1)
		go func(idx int, prompt string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := r.querier.Query(ctx, prompt, maxTokens)
			results[idx] = result
			errs[idx] = err
		}(i, p)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return "", fmt.Errorf("batch query [%d] failed: %w", i, err)
		}
	}

	encoded, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (r *REPL) callbackReadFile(args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	resolved, err := r.resolvePath(path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")

	start := 0
	if s, ok := args["start_line"].(float64); ok {
		start = int(s)
	}
	end := len(lines)
	if e, ok := args["end_line"].(float64); ok {
		end = int(e)
	}
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return "", nil
	}

	return strings.Join(lines[start:end], "\n"), nil
}

func (r *REPL) callbackGrepFile(args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	filePath, hasPath := args["path"].(string)
	var files []string
	if hasPath && filePath != "" {
		resolved, err := r.resolvePath(filePath)
		if err != nil {
			return "", err
		}
		files = []string{resolved}
	} else {
		matches, _ := filepath.Glob(filepath.Join(r.workingDir, "*"))
		for _, m := range matches {
			info, err := os.Stat(m)
			if err == nil && !info.IsDir() {
				files = append(files, m)
			}
		}
	}

	var results []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(r.workingDir, f)
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", relPath, i+1, line))
				if len(results) >= 500 {
					return strings.Join(results, "\n"), nil
				}
			}
		}
	}
	return strings.Join(results, "\n"), nil
}

func (r *REPL) callbackListFiles(args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	fullPattern := filepath.Join(r.workingDir, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}

	var relPaths []string
	for _, m := range matches {
		rel, _ := filepath.Rel(r.workingDir, m)
		relPaths = append(relPaths, rel)
	}
	return strings.Join(relPaths, "\n"), nil
}

func (r *REPL) resolvePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	var abs string
	if filepath.IsAbs(cleaned) {
		abs = cleaned
	} else {
		var err error
		abs, err = filepath.Abs(filepath.Join(r.workingDir, cleaned))
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
	}

	wdAbs, _ := filepath.Abs(r.workingDir)
	rel, err := filepath.Rel(wdAbs, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside working directory: %s", path)
	}
	return abs, nil
}
