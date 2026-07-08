package rlm

import (
	"context"
	"errors"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"
	"testing"
)

func TestRLMToolReturnsAnswer(t *testing.T) {
	runFn := func(ctx context.Context, prompt string, maxIter int) (string, error) {
		return "processed: " + prompt, nil
	}
	tool := NewRLMTool(runFn)

	result, err := tool.Execute(context.Background(), tooldef.ExecutionContext{
		Arguments: []string{`{"prompt":"big input"}`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Output)
	}
	if result.Output != "processed: big input" {
		t.Fatalf("got %q, want %q", result.Output, "processed: big input")
	}
}

func TestRLMToolError(t *testing.T) {
	runFn := func(ctx context.Context, prompt string, maxIter int) (string, error) {
		return "", errors.New("engine failed")
	}
	tool := NewRLMTool(runFn)

	result, err := tool.Execute(context.Background(), tooldef.ExecutionContext{
		Arguments: []string{`{"prompt":"test"}`},
	})
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true")
	}
	if result.Output != "rlm error: engine failed" {
		t.Fatalf("got %q", result.Output)
	}
}

func TestRLMToolEmptyPrompt(t *testing.T) {
	tool := NewRLMTool(nil)

	result, err := tool.Execute(context.Background(), tooldef.ExecutionContext{
		Arguments: []string{`{"prompt":""}`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty prompt")
	}
}

func TestRLMToolSchema(t *testing.T) {
	tool := NewRLMTool(nil)
	if tool.Name() != "rlm" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "rlm")
	}
	schema := tool.Schema()
	if _, ok := schema.Properties["prompt"]; !ok {
		t.Fatal("schema missing 'prompt' property")
	}
	if _, ok := schema.Properties["max_iterations"]; !ok {
		t.Fatal("schema missing 'max_iterations' property")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "prompt" {
		t.Fatalf("Required = %v, want [prompt]", schema.Required)
	}
}
