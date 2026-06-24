package rlm

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"tenzing-agent/internal/provider"
)

//go:embed bootstrap.py
var bootstrapScript string

type REPL struct {
	cmd        *exec.Cmd
	stdin      *json.Encoder
	scanner    *bufio.Scanner
	workingDir string
	subLLM     provider.LLM
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

	Func string         `json:"func,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	Result string `json:"result,omitempty"`
}

func NewREPL(subLLM provider.LLM, workingDir string) (*REPL, error) {
	cmd := exec.Command("python3", "-u", "-c", bootstrapScript)
	cmd.Dir = workingDir
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

	return &REPL{
		cmd:        cmd,
		stdin:      json.NewEncoder(stdinPipe),
		scanner:    bufio.NewScanner(stdoutPipe),
		workingDir: workingDir,
		subLLM:     subLLM,
	}, nil
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
	if r.subLLM == nil {
		return "", fmt.Errorf("sub_lm not available: no LLM configured")
	}
	prompt, _ := args["prompt"].(string)
	maxTokens := int64(4096)
	if mt, ok := args["max_tokens"].(float64); ok {
		maxTokens = int64(mt)
	}

	resp, err := r.subLLM.SendSyncMessage(ctx, provider.CompletionRequest{
		Model:     r.subLLM.GetCurrentModel(),
		System:    "Answer concisely and accurately.",
		Messages:  []provider.Message{provider.NewUserMessage(prompt)},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
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
	if filepath.IsAbs(cleaned) {
		if !strings.HasPrefix(cleaned, r.workingDir) {
			return "", fmt.Errorf("path outside working directory: %s", path)
		}
		return cleaned, nil
	}

	resolved := filepath.Join(r.workingDir, cleaned)
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	wdAbs, _ := filepath.Abs(r.workingDir)
	if !strings.HasPrefix(abs, wdAbs) {
		return "", fmt.Errorf("path outside working directory: %s", path)
	}
	return abs, nil
}
