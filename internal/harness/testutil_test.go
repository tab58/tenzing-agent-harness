package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/tools/tooldef"
	"tenzing-agent/internal/provider"
)

// testEventCollector captures emitted events for assertion in tests.
type testEventCollector struct {
	mu     sync.Mutex
	evts   []events.Event
}

func (c *testEventCollector) Emit(ev events.Event) {
	c.mu.Lock()
	c.evts = append(c.evts, ev)
	c.mu.Unlock()
}

func (c *testEventCollector) byType(t events.EventType) []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []events.Event
	for _, ev := range c.evts {
		if ev.Type() == t {
			out = append(out, ev)
		}
	}
	return out
}

// ScriptedAgent replays a sequence of ReasoningResults in order.
// Each DoReasoning call returns the next scripted result.
// Captures all inputs for assertion.
type ScriptedAgent struct {
	mu        sync.Mutex
	steps     []runner.ReasoningResult
	callIndex int
	captured  []capturedCall
}

type capturedCall struct {
	Inputs    []string
	Reminders []string
}

func newScriptedAgent(steps ...runner.ReasoningResult) *ScriptedAgent {
	return &ScriptedAgent{steps: steps}
}

func (s *ScriptedAgent) UpdateToolDefinitions(_ []provider.ToolDefinition)                    {}
func (s *ScriptedAgent) UpdateSkillMap(_ map[string]string)                                   {}
func (s *ScriptedAgent) UpdateOffloadFn(_ func(context.Context, string) (string, error)) {}
func (s *ScriptedAgent) UpdateStreamCallback(_ func(string))                             {}
func (s *ScriptedAgent) UpdateThinkingCallback(_ func(string))                            {}

func (s *ScriptedAgent) DoReasoning(_ context.Context, inputs []string, reminders []string) (runner.ReasoningResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.captured = append(s.captured, capturedCall{
		Inputs:    append([]string{}, inputs...),
		Reminders: append([]string{}, reminders...),
	})

	if s.callIndex >= len(s.steps) {
		return runner.ReasoningResult{}, fmt.Errorf("ScriptedAgent: no more steps (called %d times, only %d steps)", s.callIndex+1, len(s.steps))
	}

	result := s.steps[s.callIndex]
	s.callIndex++
	return result, nil
}

func (s *ScriptedAgent) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.captured)
}

func (s *ScriptedAgent) capturedCalls() []capturedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]capturedCall, len(s.captured))
	copy(out, s.captured)
	return out
}

// step builders

func toolStep(name, input string) runner.ReasoningResult {
	return runner.ReasoningResult{
		ToolCall: &tooldef.ToolCall{Name: name, Input: input},
	}
}

func finalStep(answer string) runner.ReasoningResult {
	return runner.ReasoningResult{FinalAnswer: answer}
}

func jsonInput(fields map[string]any) string {
	data, _ := json.Marshal(fields)
	return string(data)
}

// assertion helpers

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("file %s does not contain %q\ngot: %s", path, want, string(data))
	}
}

func assertFileEquals(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	if string(data) != want {
		t.Errorf("file %s content mismatch\nwant: %q\ngot:  %q", path, want, string(data))
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("file %s should not exist", path)
	}
}

func assertCallCount(t *testing.T, agent *ScriptedAgent, want int) {
	t.Helper()
	got := agent.callCount()
	if got != want {
		t.Errorf("agent call count: got %d, want %d", got, want)
	}
}

func assertAnswerContains(t *testing.T, answer, want string) {
	t.Helper()
	if !strings.Contains(answer, want) {
		t.Errorf("answer does not contain %q\ngot: %s", want, answer)
	}
}

func seedFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	return path
}
