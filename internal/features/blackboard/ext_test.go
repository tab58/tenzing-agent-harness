package blackboard

import (
	"context"
	"strings"
	"testing"
)

func TestToolsReturnsREPLSpec(t *testing.T) {
	bb := New(Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { _ = bb.Close() })
	ext := NewExt(bb, "main")

	specs := ext.Tools()
	if len(specs) != 1 {
		t.Fatalf("expected 1 tool spec, got %d", len(specs))
	}
	if specs[0].Definition.Name != "repl" {
		t.Errorf("tool name = %q, want repl", specs[0].Definition.Name)
	}
	if specs[0].Origin != "extension:blackboard" {
		t.Errorf("origin = %q, want extension:blackboard", specs[0].Origin)
	}
}

func TestSessionEndClosesBlackboard(t *testing.T) {
	bb := New(Config{WorkingDir: t.TempDir()})
	ext := NewExt(bb, "main")

	if err := ext.SessionEnd(context.Background()); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	// After Close, Execute must report the blackboard as closed.
	_, err := bb.Execute(context.Background(), "main", "print(1)")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Execute after SessionEnd: err = %v, want closed error", err)
	}
}
