package rlm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func skipIfNoPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found on PATH")
	}
}

type fakeQuerier struct {
	response string
	err      error

	mu          sync.Mutex // llm_batch calls Query concurrently
	lastPrompt  string
	lastMaxToks int64
	callCount   int
}

func (q *fakeQuerier) Query(_ context.Context, prompt string, maxTokens int64) (string, error) {
	q.mu.Lock()
	q.callCount++
	q.lastPrompt = prompt
	q.lastMaxToks = maxTokens
	q.mu.Unlock()
	if q.err != nil {
		return "", q.err
	}
	return q.response, nil
}

type multiResponseQuerier struct {
	responses []string
	idx       int
}

func (q *multiResponseQuerier) Query(_ context.Context, _ string, _ int64) (string, error) {
	if q.idx >= len(q.responses) {
		return "exhausted", nil
	}
	resp := q.responses[q.idx]
	q.idx++
	return resp, nil
}

func TestREPLPrint(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, done, final, err := r.Execute(context.Background(), `print("hello world")`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if done {
		t.Fatal("unexpected done")
	}
	if final != "" {
		t.Fatalf("unexpected final: %q", final)
	}
	if strings.TrimSpace(stdout) != "hello world" {
		t.Fatalf("stdout = %q, want %q", stdout, "hello world\n")
	}
}

func TestREPLPromptVariable(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	if err := r.SetVar("prompt", "the quick brown fox"); err != nil {
		t.Fatalf("SetVar: %v", err)
	}

	stdout, _, _, err := r.Execute(context.Background(), `print(len(prompt))`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "19" {
		t.Fatalf("stdout = %q, want %q", stdout, "19")
	}
}

func TestREPLSubLM(t *testing.T) {
	skipIfNoPython(t)
	q := &fakeQuerier{response: "the answer is 42"}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), "result = sub_lm(\"what is the answer?\")\nprint(result)")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "the answer is 42" {
		t.Fatalf("stdout = %q, want %q", stdout, "the answer is 42")
	}
	if q.lastPrompt != "what is the answer?" {
		t.Fatalf("prompt = %q, want %q", q.lastPrompt, "what is the answer?")
	}
}

func TestREPLSubLMInLoop(t *testing.T) {
	skipIfNoPython(t)
	q := &multiResponseQuerier{responses: []string{"summary-a", "summary-b", "summary-c"}}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `chunks = ["aaa", "bbb", "ccc"]
results = []
for chunk in chunks:
    results.append(sub_lm("summarize: " + chunk))
print("|".join(results))`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "summary-a|summary-b|summary-c" {
		t.Fatalf("stdout = %q, want %q", stdout, "summary-a|summary-b|summary-c")
	}
}

func TestREPLSubLMError(t *testing.T) {
	skipIfNoPython(t)
	q := &fakeQuerier{err: fmt.Errorf("api down")}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `
try:
    sub_lm("hello")
except RuntimeError as e:
    print("caught: " + str(e))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "caught: api down") {
		t.Fatalf("stdout = %q, want error caught", stdout)
	}
}

func TestREPLReadFile(t *testing.T) {
	skipIfNoPython(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("line1\nline2\nline3\n"), 0644)

	r, err := NewREPL(nil, dir, nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `content = read_file("test.txt")
print(content)`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "line1") || !strings.Contains(stdout, "line3") {
		t.Fatalf("stdout = %q, want file contents", stdout)
	}
}

func TestREPLReadFileLineRange(t *testing.T) {
	skipIfNoPython(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("line0\nline1\nline2\nline3\nline4\n"), 0644)

	r, err := NewREPL(nil, dir, nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `content = read_file("test.txt", 1, 3)
print(content)`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "line1\nline2" {
		t.Fatalf("stdout = %q, want %q", trimmed, "line1\nline2")
	}
}

func TestREPLReadFilePathTraversal(t *testing.T) {
	skipIfNoPython(t)
	dir := t.TempDir()
	r, err := NewREPL(nil, dir, nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `
try:
    read_file("../../etc/passwd")
except RuntimeError as e:
    print("blocked: " + str(e))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "blocked") {
		t.Fatalf("path traversal not blocked, stdout = %q", stdout)
	}
}

func TestREPLFinal(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	_, done, final, err := r.Execute(context.Background(), `FINAL("the answer")`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !done {
		t.Fatal("expected done=true")
	}
	if final != "the answer" {
		t.Fatalf("final = %q, want %q", final, "the answer")
	}
}

func TestREPLFinalVar(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	r.Execute(context.Background(), `results = [1, 2, 3]`)
	_, done, final, err := r.Execute(context.Background(), `FINAL_VAR("results")`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !done {
		t.Fatal("expected done=true")
	}
	if final != "[1, 2, 3]" {
		t.Fatalf("final = %q, want %q", final, "[1, 2, 3]")
	}
}

func TestREPLStatePersists(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	r.Execute(context.Background(), `x = 42`)
	stdout, _, _, err := r.Execute(context.Background(), `print(x)`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "42" {
		t.Fatalf("stdout = %q, want %q", stdout, "42")
	}
}

func TestREPLPythonError(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, done, _, err := r.Execute(context.Background(), `this is not valid python`)
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if done {
		t.Fatal("syntax error should not set done")
	}
	if !strings.Contains(stdout, "[Python Error]") {
		t.Fatalf("stdout = %q, want [Python Error]", stdout)
	}
}

func TestREPLBlocksImport(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	tests := []struct {
		name string
		code string
	}{
		{"import_os", `import os`},
		{"open_file", `open("/tmp/test_rlm_sandbox", "w")`},
		{"eval", `eval("1+1")`},
		{"exec", `exec("x=1")`},
		{"__import__", `__import__("os")`},
		{"compile", `compile("1+1", "<>", "eval")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, done, _, err := r.Execute(context.Background(), tt.code)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if done {
				t.Fatal("blocked code should not set done")
			}
			if !strings.Contains(stdout, "[Python Error]") {
				t.Fatalf("expected error for %q, got stdout = %q", tt.code, stdout)
			}
		})
	}
}

func TestREPLOSSandboxBlocksFileCreate(t *testing.T) {
	skipIfNoPython(t)
	if runtime.GOOS != "darwin" {
		t.Skip("OS-level sandbox test only runs on macOS")
	}

	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	// Escape builtins restriction via object traversal, then try to create a file.
	// sandbox-exec denies file-write-create even when Python builtins are bypassed.
	code := `
import_fn = [c for c in ().__class__.__bases__[0].__subclasses__()
             if 'BuiltinImporter' in str(c)]
if import_fn:
    bi = import_fn[0]
    os_mod = bi.load_module('os')
    try:
        fd = os_mod.open("/tmp/_rlm_sandbox_test", os_mod.O_WRONLY | os_mod.O_CREAT, 0o644)
        os_mod.close(fd)
        print("FAIL: file created")
    except OSError as e:
        print("BLOCKED: " + str(e))
else:
    print("SKIP: no BuiltinImporter found")
`
	stdout, _, _, err := r.Execute(context.Background(), code)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stdout = strings.TrimSpace(stdout)
	if strings.Contains(stdout, "FAIL") {
		os.Remove("/tmp/_rlm_sandbox_test")
		t.Fatal("OS sandbox did not block file creation")
	}
	if !strings.Contains(stdout, "BLOCKED") && !strings.Contains(stdout, "SKIP") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

type echoQuerier struct{}

func (q *echoQuerier) Query(_ context.Context, prompt string, _ int64) (string, error) {
	return "echo:" + prompt, nil
}

func TestREPLBatchSubLM(t *testing.T) {
	skipIfNoPython(t)
	q := &echoQuerier{}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `
results = llm_batch(["alpha", "beta", "gamma"])
print("|".join(results))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "echo:alpha|echo:beta|echo:gamma" {
		t.Fatalf("stdout = %q, want %q", strings.TrimSpace(stdout), "echo:alpha|echo:beta|echo:gamma")
	}
}

func TestREPLBatchSubLMEmpty(t *testing.T) {
	skipIfNoPython(t)
	q := &echoQuerier{}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `
results = llm_batch([])
print(len(results))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(stdout) != "0" {
		t.Fatalf("stdout = %q, want %q", strings.TrimSpace(stdout), "0")
	}
}

func TestREPLBatchSubLMError(t *testing.T) {
	skipIfNoPython(t)
	q := &fakeQuerier{err: fmt.Errorf("api down")}
	r, err := NewREPL(q, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, _, _, err := r.Execute(context.Background(), `
try:
    llm_batch(["hello", "world"])
except RuntimeError as e:
    print("caught: " + str(e))
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "caught:") {
		t.Fatalf("expected error caught, stdout = %q", stdout)
	}
}

func TestREPLExecTimeout(t *testing.T) {
	skipIfNoPython(t)
	if runtime.GOOS == "windows" {
		t.Skip("signal.alarm not available on Windows")
	}
	if testing.Short() {
		t.Skip("timeout test requires 30s wait")
	}
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	defer r.Close()

	stdout, done, _, err := r.Execute(context.Background(), "while True: pass")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if done {
		t.Fatal("timeout should not set done")
	}
	if !strings.Contains(stdout, "[Timeout]") {
		t.Fatalf("expected timeout message, got %q", stdout)
	}

	// REPL should still work after timeout
	stdout, _, _, err = r.Execute(context.Background(), `print("still alive")`)
	if err != nil {
		t.Fatalf("Execute after timeout: %v", err)
	}
	if strings.TrimSpace(stdout) != "still alive" {
		t.Fatalf("stdout = %q, want %q", strings.TrimSpace(stdout), "still alive")
	}
}

func TestREPLClose(t *testing.T) {
	skipIfNoPython(t)
	r, err := NewREPL(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewREPL: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
