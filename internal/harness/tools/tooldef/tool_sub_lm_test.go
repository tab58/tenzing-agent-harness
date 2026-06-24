package tooldef

import (
	"context"
	"errors"
	"testing"
)

func TestSubLMReturnsResponse(t *testing.T) {
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		return "the answer is 42", nil
	}
	tool := NewSubLMTool(queryFn)

	result, err := tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`{"prompt":"what is the answer?"}`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Output)
	}
	if result.Output != "the answer is 42" {
		t.Fatalf("got %q, want %q", result.Output, "the answer is 42")
	}
}

func TestSubLMDefaultMaxTokens(t *testing.T) {
	var capturedMaxTokens int64
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		capturedMaxTokens = maxTokens
		return "ok", nil
	}
	tool := NewSubLMTool(queryFn)

	tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`{"prompt":"hello"}`},
	})
	if capturedMaxTokens != 4096 {
		t.Fatalf("default max_tokens = %d, want 4096", capturedMaxTokens)
	}
}

func TestSubLMCustomMaxTokens(t *testing.T) {
	var capturedMaxTokens int64
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		capturedMaxTokens = maxTokens
		return "ok", nil
	}
	tool := NewSubLMTool(queryFn)

	tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`{"prompt":"hello","max_tokens":8192}`},
	})
	if capturedMaxTokens != 8192 {
		t.Fatalf("custom max_tokens = %d, want 8192", capturedMaxTokens)
	}
}

func TestSubLMErrorHandling(t *testing.T) {
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		return "", errors.New("api down")
	}
	tool := NewSubLMTool(queryFn)

	result, err := tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`{"prompt":"hello"}`},
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for queryFn failure")
	}
	if result.Output != "sub_lm error: api down" {
		t.Fatalf("got %q, want error message", result.Output)
	}
}

func TestSubLMEmptyPrompt(t *testing.T) {
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		return "should not be called", nil
	}
	tool := NewSubLMTool(queryFn)

	result, err := tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`{"prompt":""}`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty prompt")
	}
}

func TestSubLMInvalidJSON(t *testing.T) {
	queryFn := func(ctx context.Context, prompt string, maxTokens int64) (string, error) {
		return "should not be called", nil
	}
	tool := NewSubLMTool(queryFn)

	result, err := tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{`not json`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid JSON")
	}
}

func TestSubLMMissingArguments(t *testing.T) {
	tool := NewSubLMTool(nil)

	result, err := tool.Execute(context.Background(), ExecutionContext{
		Arguments: []string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing arguments")
	}
}

func TestSubLMName(t *testing.T) {
	tool := NewSubLMTool(nil)
	if tool.Name() != "sub_lm" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "sub_lm")
	}
}

func TestSubLMSchema(t *testing.T) {
	tool := NewSubLMTool(nil)
	schema := tool.Schema()
	if _, ok := schema.Properties["prompt"]; !ok {
		t.Fatal("schema missing 'prompt' property")
	}
	if _, ok := schema.Properties["max_tokens"]; !ok {
		t.Fatal("schema missing 'max_tokens' property")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "prompt" {
		t.Fatalf("Required = %v, want [prompt]", schema.Required)
	}
}
