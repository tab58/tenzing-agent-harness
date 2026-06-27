package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/snapshot"
	"tenzing-agent/internal/harness/taskgraph"
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

	registry := tools.NewRegistry()

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
	registry := tools.NewRegistry()

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

	registry := tools.NewRegistry()
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

	registry := tools.NewRegistry()
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

	registry := tools.NewRegistry()

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

	registry := tools.NewRegistry()

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

	registry := tools.NewRegistry()

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

	registry := tools.NewRegistry()

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

	registry := tools.NewRegistry()
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

	registry := tools.NewRegistry()
	registry.Register(snapshot.NewWriteTool(snapshots))
	registry.Register(snapshot.NewRevertTool(snapshots))

	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
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
// Scenario 3: Task lifecycle — full agent loop
//
// Task tools parse JSON from Arguments[0], so they work correctly through
// AgentRunner.RunLoop. This tests the complete flow:
// create task → create dependent task → update status → list all.
// ---------------------------------------------------------------------------

func TestIntegration_TaskLifecycle(t *testing.T) {
	workDir := t.TempDir()
	tg := taskgraph.NewTaskGraph(workDir)

	// We'll create tasks outside the loop to get known IDs,
	// then drive the loop for update + list.
	task1JSON, err := tg.CreateTask("define handler", nil, taskgraph.TaskPriorityHigh)
	if err != nil {
		t.Fatalf("create task 1: %v", err)
	}
	var task1 taskgraph.Task
	if err := json.Unmarshal([]byte(task1JSON), &task1); err != nil {
		t.Fatalf("parse task 1: %v", err)
	}

	task2JSON, err := tg.CreateTask("register route", []string{task1.ID}, taskgraph.TaskPriorityMedium)
	if err != nil {
		t.Fatalf("create task 2: %v", err)
	}
	var task2 taskgraph.Task
	if err := json.Unmarshal([]byte(task2JSON), &task2); err != nil {
		t.Fatalf("parse task 2: %v", err)
	}

	// Script: update task1 to done → list tasks → final answer
	agent := newScriptedAgent(
		toolStep("task_update", jsonInput(map[string]any{
			"task_id": task1.ID,
			"status":  "done",
			"result":  "handler implemented",
		})),
		toolStep("task_list", "{}"),
		finalStep("tasks shown"),
	)

	registry := tools.NewRegistry()
	registry.Register(taskgraph.NewTaskUpdateTool(tg))
	registry.Register(taskgraph.NewTaskListTool(tg))

	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := runner.RunLoop(context.Background(), "show me the tasks")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "tasks shown" {
		t.Errorf("answer = %q, want %q", answer, "tasks shown")
	}

	// Verify task graph state
	tasksJSON, err := tg.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks error: %v", err)
	}

	var tasks []taskgraph.Task
	if err := json.Unmarshal([]byte(tasksJSON), &tasks); err != nil {
		t.Fatalf("parse tasks: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	taskMap := make(map[string]taskgraph.Task)
	for _, task := range tasks {
		taskMap[task.ID] = task
	}

	t1 := taskMap[task1.ID]
	if t1.Status != "done" {
		t.Errorf("task 1 status = %q, want %q", t1.Status, "done")
	}
	if t1.Result != "handler implemented" {
		t.Errorf("task 1 result = %q, want %q", t1.Result, "handler implemented")
	}

	t2 := taskMap[task2.ID]
	if t2.Status != "pending" {
		t.Errorf("task 2 status = %q, want %q", t2.Status, "pending")
	}
	if len(t2.DependsOn) != 1 || t2.DependsOn[0] != task1.ID {
		t.Errorf("task 2 depends_on = %v, want [%s]", t2.DependsOn, task1.ID)
	}

	// Verify agent was called correct number of times:
	// 1: initial input, 2: task_update result, 3: task_list result
	assertCallCount(t, agent, 3)

	// Verify task file persisted to disk
	taskFile := filepath.Join(workDir, taskgraph.TasksFileName)
	if _, err := os.Stat(taskFile); os.IsNotExist(err) {
		t.Fatal("task file should exist on disk")
	}
}

func TestIntegration_TaskCreate_ThroughLoop(t *testing.T) {
	workDir := t.TempDir()
	tg := taskgraph.NewTaskGraph(workDir)

	agent := newScriptedAgent(
		toolStep("task_create", jsonInput(map[string]any{
			"description": "write tests",
			"priority":    "high",
		})),
		toolStep("task_list", "{}"),
		finalStep("done"),
	)

	registry := tools.NewRegistry()
	registry.Register(taskgraph.NewTaskCreateTool(tg))
	registry.Register(taskgraph.NewTaskListTool(tg))

	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := runner.RunLoop(context.Background(), "create a task")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "done" {
		t.Errorf("answer = %q, want %q", answer, "done")
	}

	tasksJSON, err := tg.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks error: %v", err)
	}

	var tasks []taskgraph.Task
	if err := json.Unmarshal([]byte(tasksJSON), &tasks); err != nil {
		t.Fatalf("parse tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Description != "write tests" {
		t.Errorf("task description = %q, want %q", tasks[0].Description, "write tests")
	}
	if tasks[0].Priority != taskgraph.TaskPriorityHigh {
		t.Errorf("task priority = %q, want %q", tasks[0].Priority, taskgraph.TaskPriorityHigh)
	}
	if tasks[0].Status != "pending" {
		t.Errorf("task status = %q, want %q", tasks[0].Status, "pending")
	}
}

func TestIntegration_TaskDependency_BlocksNext(t *testing.T) {
	workDir := t.TempDir()
	tg := taskgraph.NewTaskGraph(workDir)

	// Create two tasks, second depends on first
	t1JSON, err := tg.CreateTask("step 1", nil, taskgraph.TaskPriorityHigh)
	if err != nil {
		t.Fatalf("create task 1: %v", err)
	}
	var t1 taskgraph.Task
	json.Unmarshal([]byte(t1JSON), &t1)

	_, err = tg.CreateTask("step 2", []string{t1.ID}, taskgraph.TaskPriorityHigh)
	if err != nil {
		t.Fatalf("create task 2: %v", err)
	}

	// NextTask should return task 1 (task 2 blocked by dependency)
	nextJSON, err := tg.NextTask()
	if err != nil {
		t.Fatalf("NextTask error: %v", err)
	}
	var next taskgraph.Task
	json.Unmarshal([]byte(nextJSON), &next)
	if next.ID != t1.ID {
		t.Errorf("next task = %s, want %s", next.ID, t1.ID)
	}

	// Complete task 1
	if err := tg.UpdateTask(t1.ID, "done", ""); err != nil {
		t.Fatalf("update task 1: %v", err)
	}

	// Now NextTask should return task 2
	nextJSON, err = tg.NextTask()
	if err != nil {
		t.Fatalf("NextTask error: %v", err)
	}
	json.Unmarshal([]byte(nextJSON), &next)
	if next.Description != "step 2" {
		t.Errorf("next task description = %q, want %q", next.Description, "step 2")
	}
}

// ---------------------------------------------------------------------------
// Agent loop mechanics
// ---------------------------------------------------------------------------

func TestIntegration_FinalAnswerOnly(t *testing.T) {
	workDir := t.TempDir()
	agent := newScriptedAgent(finalStep("direct answer"))

	registry := tools.NewRegistry()
	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
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

	registry := tools.NewRegistry()
	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
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

	registry := tools.NewRegistry()
	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
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
	tg := taskgraph.NewTaskGraph(workDir)

	// Three tool calls then final answer
	agent := newScriptedAgent(
		toolStep("task_create", jsonInput(map[string]any{"description": "task A"})),
		toolStep("task_create", jsonInput(map[string]any{"description": "task B"})),
		toolStep("task_create", jsonInput(map[string]any{"description": "task C"})),
		toolStep("task_list", "{}"),
		finalStep("created 3 tasks"),
	)

	registry := tools.NewRegistry()
	registry.Register(taskgraph.NewTaskCreateTool(tg))
	registry.Register(taskgraph.NewTaskListTool(tg))

	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	answer, err := runner.RunLoop(context.Background(), "create 3 tasks")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if answer != "created 3 tasks" {
		t.Errorf("answer = %q, want %q", answer, "created 3 tasks")
	}

	tasksJSON, err := tg.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks error: %v", err)
	}
	var tasks []taskgraph.Task
	json.Unmarshal([]byte(tasksJSON), &tasks)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// 5 agent calls: initial + 3 tool results + task_list result
	assertCallCount(t, agent, 5)
}

func TestIntegration_ToolHookCalled(t *testing.T) {
	workDir := t.TempDir()
	tg := taskgraph.NewTaskGraph(workDir)

	agent := newScriptedAgent(
		toolStep("task_create", jsonInput(map[string]any{"description": "hooked task"})),
		finalStep("done"),
	)

	collector := &testEventCollector{}
	registry := tools.NewRegistry()
	registry.Register(taskgraph.NewTaskCreateTool(tg))

	runner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          agent,
		Emitter:        collector,
		ToolRegistry:   registry,
		SkillsRegistry: skills.NewRegistry(),
		TodoFile:       todo.NewTodoItemFile(workDir),
		SystemPrompt:   "test",
	})
	if err != nil {
		t.Fatalf("NewAgentRunner error: %v", err)
	}

	_, err = runner.RunLoop(context.Background(), "create task")
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}

	succeeded := collector.byType(events.EventToolSucceeded)
	if len(succeeded) != 1 {
		t.Errorf("expected 1 ToolSucceeded event, got %d", len(succeeded))
	} else if ev, ok := succeeded[0].(events.ToolSucceededEvent); !ok || ev.ToolName != "task_create" {
		t.Errorf("expected ToolSucceeded for task_create, got %v", succeeded[0])
	}
}
