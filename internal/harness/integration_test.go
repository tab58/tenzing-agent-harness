package harness

import (
	"context"
	"path/filepath"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/snapshot"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
)

// ---------------------------------------------------------------------------
// Scenario 1: File tools — Read, Edit, Write, Revert
//
// All tools parse JSON from Arguments[0], matching the format that LLMs
// produce. Tests exercise both direct registry calls and full agent loop.
// ---------------------------------------------------------------------------

func TestIntegration_ReadTool(t *testing.T) {
	workDir := t.TempDir()
	content := "line one\nline two\nline three\n"
	filePath := seedFile(t, workDir, "sample.txt", content)

	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Read", jsonInput(map[string]any{
		"file_path": filePath,
	}))
	if err != nil {
		t.Fatalf("Read tool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Read returned error: %s", result.Output)
	}

	assertAnswerContains(t, result.Output, "line one")
	assertAnswerContains(t, result.Output, "line two")
	assertAnswerContains(t, result.Output, "line three")
}

func TestIntegration_ReadTool_MissingFile(t *testing.T) {
	workDir := t.TempDir()
	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Read", jsonInput(map[string]any{
		"file_path": filepath.Join(workDir, "nope.txt"),
	}))
	if err != nil {
		t.Fatalf("Read tool error: %v", err)
	}
	if !result.IsError {
		t.Fatal("Read should return error for missing file")
	}
}

func TestIntegration_WriteAndRevert(t *testing.T) {
	workDir := t.TempDir()
	original := "original content"
	filePath := seedFile(t, workDir, "target.txt", original)

	snapshots := snapshot.NewSnapshotStore()
	writeTool := snapshot.NewWriteTool(snapshots)
	revertTool := snapshot.NewRevertTool(snapshots)

	registry := tools.NewRegistry("")
	registry.Register(writeTool)
	registry.Register(revertTool)

	writeResult, err := registry.Execute(context.Background(), "Write", jsonInput(map[string]any{
		"file_path": filePath,
		"content":   "new content",
	}))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if writeResult.IsError {
		t.Fatalf("Write returned error: %s", writeResult.Output)
	}
	assertFileEquals(t, filePath, "new content")

	revertResult, err := registry.Execute(context.Background(), "Revert", jsonInput(map[string]any{
		"file_path": filePath,
	}))
	if err != nil {
		t.Fatalf("Revert error: %v", err)
	}
	if revertResult.IsError {
		t.Fatalf("Revert returned error: %s", revertResult.Output)
	}
	assertFileEquals(t, filePath, original)
}

func TestIntegration_WriteAndRevert_NoSnapshot(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "target.txt", "content")

	snapshots := snapshot.NewSnapshotStore()
	revertTool := snapshot.NewRevertTool(snapshots)

	registry := tools.NewRegistry("")
	registry.Register(revertTool)

	result, err := registry.Execute(context.Background(), "Revert", jsonInput(map[string]any{
		"file_path": filePath,
	}))
	if err != nil {
		t.Fatalf("Revert error: %v", err)
	}
	if !result.IsError {
		t.Fatal("Revert without snapshot should return error")
	}
	assertAnswerContains(t, result.Output, "no snapshot")
}

func TestIntegration_EditTool(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "editable.txt", "hello world")

	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Edit", jsonInput(map[string]any{
		"file_path":  filePath,
		"old_string": "hello",
		"new_string": "goodbye",
	}))
	if err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Edit returned error: %s", result.Output)
	}
	assertFileEquals(t, filePath, "goodbye world")
}

func TestIntegration_EditTool_NotFound(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "editable.txt", "hello world")

	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Edit", jsonInput(map[string]any{
		"file_path":  filePath,
		"old_string": "MISSING",
		"new_string": "nope",
	}))
	if err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	if !result.IsError {
		t.Fatal("Edit should error when old_string not found")
	}
}

func TestIntegration_EditTool_NotUnique(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "editable.txt", "aaa bbb aaa")

	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Edit", jsonInput(map[string]any{
		"file_path":  filePath,
		"old_string": "aaa",
		"new_string": "ccc",
	}))
	if err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	if !result.IsError {
		t.Fatal("Edit should error when old_string is not unique")
	}
	assertAnswerContains(t, result.Output, "not unique")
}

func TestIntegration_EditTool_ReplaceAll(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "editable.txt", "aaa bbb aaa")

	registry := tools.NewRegistry("")

	result, err := registry.Execute(context.Background(), "Edit", jsonInput(map[string]any{
		"file_path":   filePath,
		"old_string":  "aaa",
		"new_string":  "ccc",
		"replace_all": true,
	}))
	if err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Edit returned error: %s", result.Output)
	}
	assertFileEquals(t, filePath, "ccc bbb ccc")
}

func TestIntegration_WriteEditRevert_FullCycle(t *testing.T) {
	workDir := t.TempDir()
	original := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	filePath := seedFile(t, workDir, "main.go", original)

	snapshots := snapshot.NewSnapshotStore()
	writeTool := snapshot.NewWriteTool(snapshots)
	revertTool := snapshot.NewRevertTool(snapshots)

	registry := tools.NewRegistry("")
	registry.Register(writeTool)
	registry.Register(revertTool)

	// Step 1: Write overwrites and snapshots
	res, err := registry.Execute(context.Background(), "Write", jsonInput(map[string]any{
		"file_path": filePath,
		"content":   "func main() {\n\tfmt.Println(\"modified\")\n}\n",
	}))
	if err != nil || res.IsError {
		t.Fatalf("Write failed: err=%v result=%s", err, res.Output)
	}
	assertFileContains(t, filePath, "modified")

	// Step 2: Edit changes content further
	res, err = registry.Execute(context.Background(), "Edit", jsonInput(map[string]any{
		"file_path":  filePath,
		"old_string": "modified",
		"new_string": "changed-again",
	}))
	if err != nil || res.IsError {
		t.Fatalf("Edit failed: err=%v result=%s", err, res.Output)
	}
	assertFileContains(t, filePath, "changed-again")

	// Step 3: Revert restores to pre-Write state (the snapshot), not pre-Edit
	res, err = registry.Execute(context.Background(), "Revert", jsonInput(map[string]any{
		"file_path": filePath,
	}))
	if err != nil || res.IsError {
		t.Fatalf("Revert failed: err=%v result=%s", err, res.Output)
	}
	assertFileEquals(t, filePath, original)
}

// Full agent loop: Read → Edit → Revert, all through RunLoop
func TestIntegration_ReadEditRevert_ThroughLoop(t *testing.T) {
	workDir := t.TempDir()
	original := "hello world\n"
	filePath := seedFile(t, workDir, "loopfile.txt", original)

	snapshots := snapshot.NewSnapshotStore()

	agent := newScriptedAgent(
		toolStep("Read", jsonInput(map[string]any{"file_path": filePath})),
		toolStep("Write", jsonInput(map[string]any{"file_path": filePath, "content": "goodbye world\n"})),
		toolStep("Edit", jsonInput(map[string]any{"file_path": filePath, "old_string": "goodbye", "new_string": "farewell"})),
		toolStep("Revert", jsonInput(map[string]any{"file_path": filePath})),
		finalStep("reverted"),
	)

	registry := tools.NewRegistry("")
	registry.Register(snapshot.NewWriteTool(snapshots))
	registry.Register(snapshot.NewRevertTool(snapshots))

	runner, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := runner.RunLoop(context.Background(), "read, edit, then revert")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "reverted" {
		t.Errorf("answer = %q, want %q", answer, "reverted")
	}

	// File should be back to original (Revert restores pre-Write snapshot)
	assertFileEquals(t, filePath, original)
	assertCallCount(t, agent, 5)
}

// ---------------------------------------------------------------------------
// Agent loop mechanics
// ---------------------------------------------------------------------------

func TestIntegration_FinalAnswerOnly(t *testing.T) {
	workDir := t.TempDir()
	agent := newScriptedAgent(finalStep("direct answer"))

	registry := tools.NewRegistry("")
	runner, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := runner.RunLoop(context.Background(), "hi")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "direct answer" {
		t.Errorf("answer = %q, want %q", answer, "direct answer")
	}
	assertCallCount(t, agent, 1)
}

func TestIntegration_ContextCanceled(t *testing.T) {
	workDir := t.TempDir()
	agent := newScriptedAgent(finalStep("should not reach"))

	registry := tools.NewRegistry("")
	runner, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = runner.RunLoop(ctx, "hi")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestIntegration_UnknownTool(t *testing.T) {
	workDir := t.TempDir()
	agent := newScriptedAgent(
		toolStep("nonexistent_tool", "{}"),
	)

	registry := tools.NewRegistry("")
	runner, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	// Unknown tool returns a tool-level error result, agent gets it as input
	// ScriptedAgent has no more steps after the tool call, so it errors
	_, err = runner.RunLoop(context.Background(), "do something")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestIntegration_MultipleToolCalls(t *testing.T) {
	workDir := t.TempDir()
	fileA := seedFile(t, workDir, "a.txt", "content A")
	fileB := seedFile(t, workDir, "b.txt", "content B")
	fileC := seedFile(t, workDir, "c.txt", "content C")

	// Three Read tool calls then final answer
	agent := newScriptedAgent(
		toolStep("Read", jsonInput(map[string]any{"file_path": fileA})),
		toolStep("Read", jsonInput(map[string]any{"file_path": fileB})),
		toolStep("Read", jsonInput(map[string]any{"file_path": fileC})),
		finalStep("read 3 files"),
	)

	registry := tools.NewRegistry("")

	r, err := runner.NewAgentRunner(
		agent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := r.RunLoop(context.Background(), "read 3 files")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "read 3 files" {
		t.Errorf("answer = %q, want %q", answer, "read 3 files")
	}

	// 4 agent calls: initial + 3 Read results
	assertCallCount(t, agent, 4)
}

func TestIntegration_ToolHookCalled(t *testing.T) {
	workDir := t.TempDir()
	filePath := seedFile(t, workDir, "hook.txt", "hook content")

	agent := newScriptedAgent(
		toolStep("Read", jsonInput(map[string]any{"file_path": filePath})),
		finalStep("done"),
	)

	collector := &testEventCollector{}
	registry := tools.NewRegistry("")

	r, err := runner.NewAgentRunner(
		agent,
		runner.WithEmitter(collector),
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skills.NewRegistry()),
		runner.WithTodoFile(todo.NewTodoFile(workDir)),
		runner.WithSystemPrompt("test"),
	)
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	_, err = r.RunLoop(context.Background(), "read file")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}

	succeeded := collector.byType(events.EventToolSucceeded)
	if len(succeeded) != 1 {
		t.Errorf("expected 1 ToolSucceeded event, got %d", len(succeeded))
	} else if ev, ok := succeeded[0].(events.ToolSucceededEvent); !ok || ev.ToolName != "Read" {
		t.Errorf("expected ToolSucceeded for Read, got %v", succeeded[0])
	}
}
