package rlm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tenzing-agent/internal/provider"
)

func skipIfNoPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found on PATH")
	}
}

type fakeLLM struct {
	response    string
	err         error
	lastPrompt  string
	lastMaxToks int64
	callCount   int
}

func (f *fakeLLM) SendSyncMessage(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	f.callCount++
	if len(req.Messages) > 0 {
		f.lastPrompt = req.Messages[0].Content[0].Text
	}
	f.lastMaxToks = req.MaxTokens
	if f.err != nil {
		return provider.CompletionResponse{}, f.err
	}
	return provider.CompletionResponse{
		Content: []provider.ContentBlock{provider.NewTextContent(f.response)},
	}, nil
}

func (f *fakeLLM) SendStreamingMessage(context.Context, provider.CompletionRequest, chan<- provider.StreamEvent) error {
	return provider.ErrNotSupported
}

func (f *fakeLLM) SendMessageWithTools(_ context.Context, _ provider.CompletionRequest, _ []provider.ToolDefinition) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, provider.ErrNotSupported
}

func (f *fakeLLM) CountTokens(context.Context, provider.CompletionRequest) (provider.TokenCount, error) {
	return provider.TokenCount{}, provider.ErrNotSupported
}

func (f *fakeLLM) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return nil, provider.ErrNotSupported
}

func (f *fakeLLM) GetCurrentModel() string      { return "fake-model" }
func (f *fakeLLM) GetContextWindowSize() int { return 128_000 }

type multiResponseLLM struct {
	fakeLLM
	responses []string
	idx       int
}

func (m *multiResponseLLM) SendSyncMessage(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	if m.idx >= len(m.responses) {
		return provider.CompletionResponse{
			Content: []provider.ContentBlock{provider.NewTextContent("exhausted")},
		}, nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return provider.CompletionResponse{
		Content: []provider.ContentBlock{provider.NewTextContent(resp)},
	}, nil
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
	llm := &fakeLLM{response: "the answer is 42"}
	r, err := NewREPL(llm, t.TempDir(), nil)
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
	if llm.lastPrompt != "what is the answer?" {
		t.Fatalf("prompt = %q, want %q", llm.lastPrompt, "what is the answer?")
	}
}

func TestREPLSubLMInLoop(t *testing.T) {
	skipIfNoPython(t)
	multi := &multiResponseLLM{responses: []string{"summary-a", "summary-b", "summary-c"}}
	r, err := NewREPL(multi, t.TempDir(), nil)
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
	llm := &fakeLLM{err: fmt.Errorf("api down")}
	r, err := NewREPL(llm, t.TempDir(), nil)
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
