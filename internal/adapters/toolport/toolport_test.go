package toolport

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"
)

// fakeTool implements tooldef.Definition for testing.
type fakeTool struct {
	name    string
	execFn  func(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error)
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "fake tool for testing" }
func (f *fakeTool) Schema() tooldef.Schema {
	return tooldef.Schema{Properties: map[string]tooldef.SchemaProperty{"input": {Type: tooldef.JsonTypeString}}}
}
func (f *fakeTool) Execute(ctx context.Context, exctx tooldef.ExecutionContext) (tooldef.ToolResult, error) {
	return f.execFn(ctx, exctx)
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		tool        *fakeTool
		wantIsError bool
		wantSubstr  string
	}{
		{
			name: "panicking tool returns error result",
			tool: &fakeTool{
				name: "panic_tool",
				execFn: func(_ context.Context, _ tooldef.ExecutionContext) (tooldef.ToolResult, error) {
					panic("boom")
				},
			},
			wantIsError: true,
			wantSubstr:  "panicked",
		},
		{
			name: "error-returning tool returns error result",
			tool: &fakeTool{
				name: "error_tool",
				execFn: func(_ context.Context, _ tooldef.ExecutionContext) (tooldef.ToolResult, error) {
					return tooldef.ToolResult{}, fmt.Errorf("disk full")
				},
			},
			wantIsError: true,
			wantSubstr:  "disk full",
		},
		{
			name: "successful tool returns output",
			tool: &fakeTool{
				name: "ok_tool",
				execFn: func(_ context.Context, _ tooldef.ExecutionContext) (tooldef.ToolResult, error) {
					return tooldef.NewToolResult("hello"), nil
				},
			},
			wantIsError: false,
			wantSubstr:  "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := tools.NewRegistry("")
			if err := reg.Register(tt.tool); err != nil {
				t.Fatal(err)
			}
			port := Wrap(reg)
			call := core.ToolCall{ID: "use-1", Name: tt.tool.name, Input: `{"input":"test"}`}
			result := port.Execute(context.Background(), call)

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v", result.IsError, tt.wantIsError)
			}
			if !strings.Contains(result.Output, tt.wantSubstr) {
				t.Errorf("Output = %q, want substring %q", result.Output, tt.wantSubstr)
			}
			if result.ToolUseID != "use-1" {
				t.Errorf("ToolUseID = %q, want %q", result.ToolUseID, "use-1")
			}
		})
	}
}

func TestOrigin(t *testing.T) {
	reg := tools.NewRegistry("")
	port := Wrap(reg)
	if got := port.Origin("anything"); got != "native" {
		t.Errorf("Origin = %q, want %q", got, "native")
	}
}
