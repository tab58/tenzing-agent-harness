package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tab58/llm-providers/common"
)

// --- fakes ----------------------------------------------------------------

// fakeModel returns scripted ReasoningResults in sequence.
type fakeModel struct {
	mu    sync.Mutex
	steps []ReasoningResult
	idx   int
}

func (f *fakeModel) DoReasoning(_ context.Context, _ []common.Message, _ []string) (ReasoningResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.steps) {
		return ReasoningResult{}, fmt.Errorf("fakeModel: no more scripted steps (called %d times, have %d)", f.idx+1, len(f.steps))
	}
	r := f.steps[f.idx]
	f.idx++
	return r, nil
}

func (f *fakeModel) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idx
}

// fakeTools records Execute calls and returns canned results.
type fakeTools struct {
	mu      sync.Mutex
	calls   []ToolCall
	results map[string]ToolResult // keyed by tool name
}

func newFakeTools(results map[string]ToolResult) *fakeTools {
	return &fakeTools{results: results}
}

func (f *fakeTools) Definitions() []common.ToolDefinition { return nil }
func (f *fakeTools) Origin(name string) string            { return "native" }

func (f *fakeTools) Execute(_ context.Context, call ToolCall) ToolResult {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if r, ok := f.results[call.Name]; ok {
		r.ToolUseID = call.ID
		return r
	}
	return ToolResult{ToolUseID: call.ID, Output: "ok"}
}

func (f *fakeTools) executed() []ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ToolCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeContext records the call order as []string for assertion.
type fakeContext struct {
	mu       sync.Mutex
	msgs     []common.Message
	callLog  []string
}

func newFakeContext() *fakeContext {
	return &fakeContext{}
}

func (f *fakeContext) Messages(_ context.Context) ([]common.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, "Messages")
	out := make([]common.Message, len(f.msgs))
	copy(out, f.msgs)
	return out, nil
}

func (f *fakeContext) AppendUser(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, "AppendUser")
	f.msgs = append(f.msgs, common.NewUserMessage(text))
	return nil
}

func (f *fakeContext) AppendAssistant(_ context.Context, msg common.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, "AppendAssistant")
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *fakeContext) AppendToolResults(_ context.Context, results []ToolResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, "AppendToolResults")
	blocks := make([]common.ContentBlock, len(results))
	for i, r := range results {
		blocks[i] = common.NewToolResultContent(r.ToolUseID, "", r.Output)
	}
	f.msgs = append(f.msgs, common.Message{Role: common.RoleTool, Content: blocks})
	return nil
}

func (f *fakeContext) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.callLog))
	copy(out, f.callLog)
	return out
}

// fakeEmitter collects events.
type fakeEmitter struct {
	mu     sync.Mutex
	events []Event
}

func (e *fakeEmitter) Emit(ev Event) {
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
}

func (e *fakeEmitter) eventTypes() []EventType {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]EventType, len(e.events))
	for i, ev := range e.events {
		out[i] = ev.Type()
	}
	return out
}

// toolCallResult builds a ReasoningResult for a tool_use response.
func toolCallResult(calls ...ToolCall) ReasoningResult {
	blocks := make([]common.ContentBlock, len(calls))
	for i, c := range calls {
		blocks[i] = common.NewToolUseContent(c.ID, c.Name, []byte(c.Input))
	}
	return ReasoningResult{
		ToolCalls: calls,
		Meta:      ResponseMeta{AssistantMessage: common.Message{Role: common.RoleAssistant, Content: blocks}},
	}
}

func newTestLoop(t *testing.T, model ModelPort, tools ToolPort, ctx ContextPort, opts ...func(*LoopConfig)) *Loop {
	t.Helper()
	cfg := LoopConfig{
		ID:      "test-loop",
		Model:   model,
		Tools:   tools,
		Context: ctx,
	}
	for _, o := range opts {
		o(&cfg)
	}
	l, err := NewLoop(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// --- tests ----------------------------------------------------------------

func assertEventOrder(t *testing.T, got, want []EventType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d\n  got:  %v\n  want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %q, want %q\n  full: %v", i, got[i], want[i], got)
		}
	}
}

func TestRunTurnFinalAnswerImmediately(t *testing.T) {
	model := &fakeModel{steps: []ReasoningResult{
		{FinalAnswer: "the answer"},
	}}
	tools := newFakeTools(nil)
	fctx := newFakeContext()
	emitter := &fakeEmitter{}

	l := newTestLoop(t, model, tools, fctx, func(cfg *LoopConfig) {
		cfg.Emitter = emitter
	})
	tr, err := l.RunTurn(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if tr.FinalAnswer != "the answer" {
		t.Errorf("FinalAnswer = %q, want %q", tr.FinalAnswer, "the answer")
	}
	if tr.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1", tr.Iterations)
	}
	if len(tools.executed()) != 0 {
		t.Errorf("expected no tool executions, got %d", len(tools.executed()))
	}

	assertEventOrder(t, emitter.eventTypes(), []EventType{
		EventTurnStarted,
		EventLoopStarted,
		EventReasoningStarted,
		EventReasoningFinished,
		EventLLMResponse,
		EventLoopStopped,
		EventTurnCompleted,
	})
}

func TestRunTurnToolCallThenAnswer(t *testing.T) {
	model := &fakeModel{steps: []ReasoningResult{
		toolCallResult(ToolCall{ID: "tc1", Name: "search", Input: `{"q":"test"}`}),
		{FinalAnswer: "found it"},
	}}
	tools := newFakeTools(map[string]ToolResult{
		"search": {Output: "result data"},
	})
	fctx := newFakeContext()
	emitter := &fakeEmitter{}

	l := newTestLoop(t, model, tools, fctx, func(cfg *LoopConfig) {
		cfg.Emitter = emitter
	})
	tr, err := l.RunTurn(context.Background(), "find something")
	if err != nil {
		t.Fatal(err)
	}
	if tr.FinalAnswer != "found it" {
		t.Errorf("FinalAnswer = %q, want %q", tr.FinalAnswer, "found it")
	}

	// Verify tool was executed
	executed := tools.executed()
	if len(executed) != 1 {
		t.Fatalf("expected 1 tool execution, got %d", len(executed))
	}
	if executed[0].Name != "search" {
		t.Errorf("executed tool = %q, want %q", executed[0].Name, "search")
	}

	// Verify context call order
	callOrder := fctx.calls()
	wantCalls := []string{"AppendUser", "Messages", "AppendAssistant", "AppendToolResults", "Messages", "AppendAssistant"}
	if len(callOrder) != len(wantCalls) {
		t.Fatalf("context calls = %v, want %v", callOrder, wantCalls)
	}
	for i := range wantCalls {
		if callOrder[i] != wantCalls[i] {
			t.Errorf("context call[%d] = %q, want %q", i, callOrder[i], wantCalls[i])
		}
	}

	// Verify event emission order across both iterations
	assertEventOrder(t, emitter.eventTypes(), []EventType{
		// iteration 1: tool call
		EventTurnStarted,
		EventLoopStarted,
		EventReasoningStarted,
		EventReasoningFinished,
		EventLLMResponse,
		EventToolExecutionStarted,
		EventToolExecutionFinished,
		EventToolSucceeded,
		// iteration 2: final answer
		EventReasoningStarted,
		EventReasoningFinished,
		EventLLMResponse,
		EventLoopStopped,
		EventTurnCompleted,
	})
}

// denyExtension denies every tool call.
type denyExtension struct{}

func (denyExtension) Name() string { return "deny-all" }
func (denyExtension) OnToolCall(_ context.Context, tcc *ToolCallContext) error {
	tcc.Decision = Deny
	tcc.Reason = "test policy"
	return nil
}

func TestRunTurnDenyDecision(t *testing.T) {
	model := &fakeModel{steps: []ReasoningResult{
		toolCallResult(ToolCall{ID: "tc1", Name: "dangerous", Input: `{}`}),
		{FinalAnswer: "denied result"},
	}}
	tools := newFakeTools(nil)
	fctx := newFakeContext()
	exts := NewExtensions(denyExtension{})

	l := newTestLoop(t, model, tools, fctx, func(cfg *LoopConfig) {
		cfg.Extensions = exts
	})
	tr, err := l.RunTurn(context.Background(), "do something dangerous")
	if err != nil {
		t.Fatal(err)
	}
	if tr.FinalAnswer != "denied result" {
		t.Errorf("FinalAnswer = %q, want %q", tr.FinalAnswer, "denied result")
	}

	// Tool must NOT have been executed
	if len(tools.executed()) != 0 {
		t.Errorf("expected tool NOT to be executed, got %d executions", len(tools.executed()))
	}
}

// terminateExtension sets Terminate on the first iteration.
type terminateExtension struct {
	reason string
}

func (te terminateExtension) Name() string { return "terminator" }
func (te terminateExtension) BeforeIteration(_ context.Context, tc *TurnContext) error {
	tc.Terminate = te.reason
	return nil
}

func TestRunTurnHookTerminate(t *testing.T) {
	model := &fakeModel{steps: []ReasoningResult{
		{FinalAnswer: "should not reach"},
	}}
	tools := newFakeTools(nil)
	fctx := newFakeContext()
	exts := NewExtensions(terminateExtension{reason: "budget exhausted"})

	l := newTestLoop(t, model, tools, fctx, func(cfg *LoopConfig) {
		cfg.Extensions = exts
	})
	tr, err := l.RunTurn(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Terminated != "budget exhausted" {
		t.Errorf("Terminated = %q, want %q", tr.Terminated, "budget exhausted")
	}
	// Model should NOT have been called
	if model.calls() != 0 {
		t.Errorf("expected 0 model calls, got %d", model.calls())
	}
}

func TestRunTurnCtxCancel(t *testing.T) {
	model := &fakeModel{steps: []ReasoningResult{
		{FinalAnswer: "should not reach"},
	}}
	tools := newFakeTools(nil)
	fctx := newFakeContext()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	l := newTestLoop(t, model, tools, fctx)
	_, err := l.RunTurn(ctx, "hello")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error = %q, want substring %q", err.Error(), "canceled")
	}
}

func TestRunTurnInvalidFinalRetry(t *testing.T) {
	tests := []struct {
		name       string
		steps      []ReasoningResult
		wantAnswer string
		wantCalls  int
	}{
		{
			name: "empty then valid",
			steps: []ReasoningResult{
				{FinalAnswer: ""},
				{FinalAnswer: "real answer"},
			},
			wantAnswer: "real answer",
			wantCalls:  2,
		},
		{
			name: "pseudo tool call then valid",
			steps: []ReasoningResult{
				{FinalAnswer: "<|tool_call>call:graph_cypher{...}"},
				{FinalAnswer: "real answer"},
			},
			wantAnswer: "real answer",
			wantCalls:  2,
		},
		{
			name: "empty, pseudo tool call, then valid",
			steps: []ReasoningResult{
				{FinalAnswer: ""},
				{FinalAnswer: "<|tool_call>call:graph_cypher{...}"},
				{FinalAnswer: "Total expenses were $1.2M."},
			},
			wantAnswer: "Total expenses were $1.2M.",
			wantCalls:  3,
		},
		{
			name: "truncated then valid",
			steps: []ReasoningResult{
				{FinalAnswer: "I can see there are two values. There could", Meta: ResponseMeta{StopReason: "max_tokens"}},
				{FinalAnswer: `{"metricName": "Total Assets", "value": {"2020": "$1,051,999"}}`, Meta: ResponseMeta{StopReason: "end_turn"}},
			},
			wantAnswer: `{"metricName": "Total Assets", "value": {"2020": "$1,051,999"}}`,
			wantCalls:  2,
		},
		{
			name: "gives up after max retries",
			steps: []ReasoningResult{
				{FinalAnswer: ""},
				{FinalAnswer: ""},
				{FinalAnswer: ""},
			},
			wantAnswer: "",
			wantCalls:  1 + maxInvalidFinalRetries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &fakeModel{steps: tt.steps}
			tools := newFakeTools(nil)
			fctx := newFakeContext()

			l := newTestLoop(t, model, tools, fctx)
			tr, err := l.RunTurn(context.Background(), "test query")
			if err != nil {
				t.Fatal(err)
			}
			if tr.FinalAnswer != tt.wantAnswer {
				t.Errorf("FinalAnswer = %q, want %q", tr.FinalAnswer, tt.wantAnswer)
			}
			if model.calls() != tt.wantCalls {
				t.Errorf("model calls = %d, want %d", model.calls(), tt.wantCalls)
			}
		})
	}
}

func TestNewLoopValidation(t *testing.T) {
	model := &fakeModel{}
	tools := newFakeTools(nil)
	fctx := newFakeContext()

	tests := []struct {
		name    string
		cfg     LoopConfig
		wantErr string
	}{
		{"missing ID", LoopConfig{Model: model, Tools: tools, Context: fctx}, "loop ID"},
		{"missing Model", LoopConfig{ID: "x", Tools: tools, Context: fctx}, "ModelPort"},
		{"missing Tools", LoopConfig{ID: "x", Model: model, Context: fctx}, "ToolPort"},
		{"missing Context", LoopConfig{ID: "x", Model: model, Tools: tools}, "ContextPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLoop(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestInvalidFinalAnswerReason(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		invalid bool
	}{
		{"plain prose", "Total expenses for 2021 were $1.2M.", false},
		{"empty", "", true},
		{"whitespace only", "   \n\t ", true},
		{"gemma corruption artifact", `<|tool_call>call:graph_aggregate{query:...}<tool_call|>`, true},
		{"bare call syntax", `call:graph_cypher{query: "MATCH (n) RETURN n"}`, true},
		{"call with paren", `call:llm_query("what is revenue")`, true},
		{"prose mentioning tools", "I used the graph_cypher tool to find the answer: $500.", false},
		{"json-ish but legitimate answer", `The values are {"jan": 100, "feb": 200}.`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := invalidFinalAnswerReason(tt.answer)
			if tt.invalid && reason == "" {
				t.Errorf("expected %q to be invalid", tt.answer)
			}
			if !tt.invalid && reason != "" {
				t.Errorf("expected %q to be valid, got reason %q", tt.answer, reason)
			}
		})
	}
}
