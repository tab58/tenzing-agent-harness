package blackboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found on PATH")
	}
}

func newTestBlackboard(t *testing.T) *Blackboard {
	t.Helper()
	requirePython(t)
	bb := New(Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { _ = bb.Close() })
	return bb
}

type stubQuerier struct{}

func (stubQuerier) Query(_ context.Context, prompt string, _ int64) (string, error) {
	return "echo:" + prompt, nil
}

func TestBlackboardStatePersistsAcrossExecutes(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	if _, err := bb.Execute(ctx, "main", "x = 41"); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	out, err := bb.Execute(ctx, "main", "print(x + 1)")
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Errorf("got %q, want 42", out)
	}
}

func TestBlackboardHelpersAvailable(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	out, err := bb.Execute(ctx, "main",
		"bb['main']['note'] = 'alpha beta'\n"+
			"print(peek(bb['main']['note'], 0, 5))\n"+
			"print(bb_grep(r'beta', bb['main']['note']))")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("peek output missing: %q", out)
	}
	if !strings.Contains(out, "0:alpha beta") {
		t.Errorf("bb_grep output missing: %q", out)
	}
}

func TestBlackboardMainSlotPreexists(t *testing.T) {
	bb := newTestBlackboard(t)
	out, err := bb.Execute(context.Background(), "main", "print(sorted(bb.keys()))")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "'main'") {
		t.Errorf("bb should start with a 'main' slot, got %q", out)
	}
}

func TestBlackboardPythonErrorReturnsInStdoutNotGoError(t *testing.T) {
	bb := newTestBlackboard(t)
	out, err := bb.Execute(context.Background(), "main", "raise ValueError('boom')")
	if err != nil {
		t.Fatalf("python exceptions must not be Go errors: %v", err)
	}
	if !strings.Contains(out, "[Python Error]") || !strings.Contains(out, "boom") {
		t.Errorf("expected traceback in stdout, got %q", out)
	}
}

func TestBlackboardLLMQueryCallback(t *testing.T) {
	requirePython(t)
	bb := New(Config{WorkingDir: t.TempDir(), Querier: stubQuerier{}})
	t.Cleanup(func() { _ = bb.Close() })

	out, err := bb.Execute(context.Background(), "main", "print(llm_query('hi'))")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "echo:hi" {
		t.Errorf("got %q, want echo:hi", out)
	}
}

func TestBlackboardConcurrentExecutes(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("g%d", i)
			code := fmt.Sprintf("bb.setdefault('%s', {})['v'] = %d", id, i)
			if _, err := bb.Execute(ctx, id, code); err != nil {
				errs[i] = err
				return
			}
			out, err := bb.Execute(ctx, id, fmt.Sprintf("print(bb['%s']['v'])", id))
			if err != nil {
				errs[i] = err
				return
			}
			if strings.TrimSpace(out) != fmt.Sprintf("%d", i) {
				errs[i] = fmt.Errorf("agent %s read back %q, want %d", id, out, i)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestBlackboardSurvivesOversizedPrint(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	if _, err := bb.Execute(ctx, "main", "x = 41"); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	out, err := bb.Execute(ctx, "main", `print('y' * 2_000_000)`)
	if err != nil {
		t.Fatalf("oversized print execute: %v", err)
	}
	if !strings.Contains(out, "[stdout truncated") {
		t.Errorf("expected truncation notice, got %d chars", len(out))
	}

	out, err = bb.Execute(ctx, "main", "print(x + 1)")
	if err != nil {
		t.Fatalf("execute after oversized print: %v", err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Errorf("shared state did not survive oversized print: got %q, want 42", out)
	}
}

func TestBlackboardCloseBeforeStartIsNoop(t *testing.T) {
	bb := New(Config{WorkingDir: t.TempDir()})
	if err := bb.Close(); err != nil {
		t.Errorf("Close on unstarted blackboard: %v", err)
	}
}

func TestBlackboardExecuteAfterCloseReturnsError(t *testing.T) {
	bb := New(Config{WorkingDir: t.TempDir()})
	if err := bb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := bb.Execute(context.Background(), "main", "print(1)")
	if err == nil {
		t.Fatal("Execute after Close should return an error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error = %q, want it to mention \"closed\"", err.Error())
	}
}

func TestBlackboardDeposit(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	long := strings.Repeat("q", 5000)
	pv, err := bb.Deposit(ctx, "a1", "result", long)
	if err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if pv.Chars != 5000 {
		t.Errorf("preview chars = %d, want 5000", pv.Chars)
	}
	if pv.Tail == "" {
		t.Error("expected split preview for 5000-char value")
	}

	out, err := bb.Execute(ctx, "main", "print(len(bb['a1']['result']))")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if strings.TrimSpace(out) != "5000" {
		t.Errorf("stored length = %q, want 5000", out)
	}

	// deposit temp variable must not leak into the namespace
	out, err = bb.Execute(ctx, "main", "print('_bb_deposit' in dir())")
	if err != nil {
		t.Fatalf("leak check: %v", err)
	}
	if strings.TrimSpace(out) != "False" {
		t.Errorf("_bb_deposit leaked into namespace")
	}
}

func TestBlackboardDepositValueWithSpecialChars(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	value := "line1\n\"quoted\" 'single' \\backslash\\ é世界"
	if _, err := bb.Deposit(ctx, "a2", "result", value); err != nil {
		t.Fatalf("deposit: %v", err)
	}
	out, err := bb.Execute(ctx, "main", "print(len(bb['a2']['result']))")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	// python len() counts unicode code points, not bytes
	wantLen := fmt.Sprintf("%d", len([]rune(value)))
	if strings.TrimSpace(out) != wantLen {
		t.Errorf("stored rune length = %q, want %s", out, wantLen)
	}
}

func TestBlackboardSurvivesHugeExceptionMessage(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	if _, err := bb.Execute(ctx, "main", "x = 41"); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	out, err := bb.Execute(ctx, "main", "raise ValueError('x' * 2_000_000)")
	if err != nil {
		t.Fatalf("huge exception execute: %v", err)
	}
	if !strings.Contains(out, "[Python Error]") {
		t.Errorf("expected [Python Error] in output, got %d chars", len(out))
	}

	out, err = bb.Execute(ctx, "main", "print(x + 1)")
	if err != nil {
		t.Fatalf("execute after huge exception: %v", err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Errorf("shared state did not survive huge exception: got %q, want 42", out)
	}
}

func TestBlackboardHugeLLMQueryPromptErrsCleanly(t *testing.T) {
	requirePython(t)
	bb := New(Config{WorkingDir: t.TempDir(), Querier: stubQuerier{}})
	t.Cleanup(func() { _ = bb.Close() })
	ctx := context.Background()

	if _, err := bb.Execute(ctx, "main", "x = 41"); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	out, err := bb.Execute(ctx, "main", "print(llm_query('x' * 600_000))")
	if err != nil {
		t.Fatalf("huge prompt execute: %v", err)
	}
	if !strings.Contains(out, "llm_query prompt is") {
		t.Errorf("expected guard message in output, got %q", out)
	}

	out, err = bb.Execute(ctx, "main", "print(x + 1)")
	if err != nil {
		t.Fatalf("execute after huge prompt: %v", err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Errorf("shared state did not survive huge prompt: got %q, want 42", out)
	}
}

func TestBlackboardDepositRejectsInvalidSlotOrKey(t *testing.T) {
	bb := New(Config{WorkingDir: t.TempDir()})
	ctx := context.Background()

	tests := []struct{ slot, key string }{
		{"a1'; import os #", "result"},
		{"a1", "k']=1 #"},
		{"", "result"},
		{"a1", ""},
	}
	for _, tt := range tests {
		if _, err := bb.Deposit(ctx, tt.slot, tt.key, "v"); err == nil {
			t.Errorf("Deposit(%q, %q) succeeded, want error", tt.slot, tt.key)
		}
	}
}

// Regression: sub-agents obeyed task-prompt instructions to deposit under
// invented slot names (bb['agents_md']) instead of their own agent ID. The
// slot convention must be enforced, not advisory.
func TestBlackboardRejectsForeignSlotWrites(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	out, err := bb.Execute(ctx, "agent_a", "bb['agents_md'] = {'result': 'stolen'}")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "PermissionError") {
		t.Fatalf("foreign top-level write not rejected:\n%s", out)
	}

	out, err = bb.Execute(ctx, "main", "print('agents_md' in bb)")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "False" {
		t.Fatalf("foreign slot was created anyway: %q", out)
	}
}

func TestBlackboardOwnSlotWriteAndForeignReadAllowed(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	if out, err := bb.Execute(ctx, "agent_a", "bb['agent_a'] = {'result': 'mine'}"); err != nil || strings.Contains(out, "Error") {
		t.Fatalf("own-slot write failed: %v / %s", err, out)
	}
	// setdefault on a foreign slot must also be rejected
	if out, err := bb.Execute(ctx, "agent_b", "bb.setdefault('agent_a_fake', {})"); err != nil || !strings.Contains(out, "PermissionError") {
		t.Fatalf("foreign setdefault not rejected: %v / %s", err, out)
	}
	// reads of other slots stay open
	out, err := bb.Execute(ctx, "agent_b", "print(bb['agent_a']['result'])")
	if err != nil {
		t.Fatalf("foreign read: %v", err)
	}
	if strings.TrimSpace(out) != "mine" {
		t.Fatalf("foreign read = %q, want mine", out)
	}
}

// The Go Deposit path is trusted (the factory deposits into child slots) and
// must bypass the write guard regardless of prior REPL activity.
func TestDepositBypassesWriteGuard(t *testing.T) {
	bb := newTestBlackboard(t)
	ctx := context.Background()

	// Prime the guard with a different writer first.
	if _, err := bb.Execute(ctx, "agent_a", "x = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := bb.Deposit(ctx, "agent_c", "result", "deposited"); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	out, err := bb.Execute(ctx, "main", "print(bb['agent_c']['result'])")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "deposited" {
		t.Fatalf("deposit content = %q", out)
	}
}
