package taskgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndNext(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	created, err := g.CreateTask("first task", nil, "high")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var task Task
	if err := json.Unmarshal([]byte(created), &task); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	if task.Description != "first task" || task.Priority != "high" || task.Status != "pending" {
		t.Fatalf("unexpected task: %+v", task)
	}

	next, err := g.NextTask()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	var nextTask Task
	if err := json.Unmarshal([]byte(next), &nextTask); err != nil {
		t.Fatalf("unmarshal next: %v", err)
	}
	if nextTask.ID != task.ID {
		t.Fatalf("next returned wrong task: got %s want %s", nextTask.ID, task.ID)
	}
}

func TestPriorityOrdering(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	g.CreateTask("low task", nil, "low")
	g.CreateTask("high task", nil, "high")
	g.CreateTask("medium task", nil, "medium")

	next, err := g.NextTask()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	var task Task
	json.Unmarshal([]byte(next), &task)
	if task.Description != "high task" {
		t.Fatalf("expected high task first, got %q", task.Description)
	}
}

func TestDependencyBlocking(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	aJSON, _ := g.CreateTask("task a", nil, "medium")
	var a Task
	json.Unmarshal([]byte(aJSON), &a)

	g.CreateTask("task b (depends on a)", []string{a.ID}, "high")

	next, _ := g.NextTask()
	var nextTask Task
	json.Unmarshal([]byte(next), &nextTask)
	if nextTask.Description != "task a" {
		t.Fatalf("expected task a (unblocked), got %q", nextTask.Description)
	}

	g.UpdateTask(a.ID, "done", "completed")

	next, _ = g.NextTask()
	json.Unmarshal([]byte(next), &nextTask)
	if nextTask.Description != "task b (depends on a)" {
		t.Fatalf("expected task b after a done, got %q", nextTask.Description)
	}
}

func TestUpdatePrefixMatch(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	created, _ := g.CreateTask("some task", nil, "medium")
	var task Task
	json.Unmarshal([]byte(created), &task)

	prefix := task.ID[:4]
	if err := g.UpdateTask(prefix, "in_progress", ""); err != nil {
		t.Fatalf("prefix update: %v", err)
	}

	list, _ := g.ListTasks()
	var tasks []Task
	json.Unmarshal([]byte(list), &tasks)
	if tasks[0].Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", tasks[0].Status)
	}
}

func TestNextEmptyGraph(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	next, err := g.NextTask()
	if err != nil {
		t.Fatalf("next on empty: %v", err)
	}
	if next != "" {
		t.Fatalf("expected empty, got %q", next)
	}
}

func TestInvalidDependency(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	_, err := g.CreateTask("bad dep", []string{"nonexistent"}, "medium")
	if err == nil {
		t.Fatal("expected error for invalid dependency")
	}
}

func TestFilePersistence(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	g.CreateTask("persistent task", nil, "high")

	g2 := NewTaskGraph(dir)
	next, _ := g2.NextTask()
	if next == "" {
		t.Fatal("expected task from new graph instance")
	}

	data, err := os.ReadFile(filepath.Join(dir, TasksFileName))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var tasks []Task
	json.Unmarshal(data, &tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task on disk, got %d", len(tasks))
	}
}

func TestReminder(t *testing.T) {
	dir := t.TempDir()
	g := NewTaskGraph(dir)

	reminder := g.Reminder()
	if reminder != "" {
		t.Fatal("expected empty reminder for empty graph")
	}

	g.CreateTask("do stuff", nil, "medium")
	reminder = g.Reminder()
	if reminder == "" {
		t.Fatal("expected non-empty reminder")
	}
}
