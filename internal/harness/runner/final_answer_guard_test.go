package runner

import (
	"context"
	"testing"

	"github.com/tab58/tenzing-agent-harness/internal/harness/skills"
	"github.com/tab58/tenzing-agent-harness/internal/harness/todo"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools"
)

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

func newGuardTestRunner(t *testing.T, agent *minimalAgent) *AgentRunner {
	t.Helper()
	r, err := NewAgentRunner(
		agent,
		WithEmitter(&eventCollector{}),
		WithToolRegistry(tools.NewRegistry("")),
		WithSkillsRegistry(skills.NewRegistry()),
		WithTodoFile(todo.NewTodoStore()),
		WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunLoop_RetriesInvalidFinalAnswer(t *testing.T) {
	agent := &minimalAgent{steps: []ReasoningResult{
		{FinalAnswer: ""},                                   // empty → retry 1
		{FinalAnswer: "<|tool_call>call:graph_cypher{...}"}, // pseudo tool call → retry 2
		{FinalAnswer: "Total expenses were $1.2M."},         // valid
	}}

	r := newGuardTestRunner(t, agent)

	answer, err := r.RunLoop(context.Background(), "what were total expenses?")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Total expenses were $1.2M." {
		t.Errorf("answer = %q, want the third (valid) response", answer)
	}
}

func TestRunLoop_GivesUpAfterMaxRetries(t *testing.T) {
	// All responses invalid: after maxInvalidFinalRetries bounces, the loop
	// must return the last answer rather than spin forever.
	agent := &minimalAgent{steps: []ReasoningResult{
		{FinalAnswer: ""},
		{FinalAnswer: ""},
		{FinalAnswer: ""},
	}}

	r := newGuardTestRunner(t, agent)

	answer, err := r.RunLoop(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want empty (retries exhausted)", answer)
	}
	if agent.idx != 1+maxInvalidFinalRetries {
		t.Errorf("DoReasoning called %d times, want %d", agent.idx, 1+maxInvalidFinalRetries)
	}
}
