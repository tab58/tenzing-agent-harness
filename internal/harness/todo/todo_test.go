package todo

import (
	"strings"
	"sync"
	"testing"

	"tenzing-agent/internal/harness/events"
)

func TestWriteAndReadTasks(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	tasks := []Task{
		{ID: "aaa", Description: "first", Status: "pending", Priority: PriorityHigh, DependsOn: []string{}},
		{ID: "bbb", Description: "second", Status: "pending", Priority: PriorityMedium, DependsOn: []string{"aaa"}},
	}
	if err := tf.WriteTasks(tasks); err != nil {
		t.Fatal(err)
	}

	got, err := tf.ReadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(got))
	}
	if got[0].ID != "aaa" || got[1].ID != "bbb" {
		t.Errorf("IDs mismatch: %q, %q", got[0].ID, got[1].ID)
	}
}

func TestCreateTaskAppends(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	t1, err := tf.CreateTask("first task", nil, PriorityHigh)
	if err != nil {
		t.Fatal(err)
	}
	if t1.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if t1.Status != "pending" {
		t.Errorf("status = %q, want pending", t1.Status)
	}

	t2, err := tf.CreateTask("second task", []string{t1.ID}, PriorityMedium)
	if err != nil {
		t.Fatal(err)
	}

	tasks, _ := tf.ReadTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[1].DependsOn[0] != t1.ID {
		t.Errorf("dependency = %q, want %q", tasks[1].DependsOn[0], t1.ID)
	}
	_ = t2
}

func TestCreateTaskValidatesDependency(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	_, err := tf.CreateTask("orphan", []string{"nonexistent"}, PriorityMedium)
	if err == nil {
		t.Fatal("expected error for invalid dependency")
	}
}

func TestUpdateTask(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	task, _ := tf.CreateTask("do thing", nil, PriorityMedium)

	if err := tf.UpdateTask(task.ID, "done", "finished"); err != nil {
		t.Fatal(err)
	}

	tasks, _ := tf.ReadTasks()
	if tasks[0].Status != "done" {
		t.Errorf("status = %q, want done", tasks[0].Status)
	}
	if tasks[0].Result != "finished" {
		t.Errorf("result = %q, want finished", tasks[0].Result)
	}
}

func TestUpdateTaskByPrefix(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	task, _ := tf.CreateTask("do thing", nil, PriorityMedium)
	prefix := task.ID[:4]

	if err := tf.UpdateTask(prefix, "in_progress", ""); err != nil {
		t.Fatal(err)
	}

	tasks, _ := tf.ReadTasks()
	if tasks[0].Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", tasks[0].Status)
	}
}

func TestNextTaskRespectsDepOrder(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	t1, _ := tf.CreateTask("first", nil, PriorityMedium)
	tf.CreateTask("second", []string{t1.ID}, PriorityHigh)

	next, ok, err := tf.NextTask()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a next task")
	}
	if next.ID != t1.ID {
		t.Errorf("next = %q, want %q (dependency not done yet)", next.ID, t1.ID)
	}
}

func TestNextTaskReturnsPriorityOrder(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	tf.CreateTask("low task", nil, PriorityLow)
	high, _ := tf.CreateTask("high task", nil, PriorityHigh)

	next, ok, _ := tf.NextTask()
	if !ok {
		t.Fatal("expected a next task")
	}
	if next.ID != high.ID {
		t.Errorf("next = %q, want %q (high priority)", next.ID, high.ID)
	}
}

func TestNextTaskSkipsDone(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	t1, _ := tf.CreateTask("done task", nil, PriorityHigh)
	t2, _ := tf.CreateTask("pending task", nil, PriorityMedium)
	tf.UpdateTask(t1.ID, "done", "")

	next, ok, _ := tf.NextTask()
	if !ok {
		t.Fatal("expected a next task")
	}
	if next.ID != t2.ID {
		t.Errorf("next = %q, want %q", next.ID, t2.ID)
	}
}

func TestNextTaskNoneAvailable(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	_, ok, err := tf.NextTask()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no next task on empty list")
	}
}

func TestFormatReminderTopologicalOrder(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	t1, _ := tf.CreateTask("foundation", nil, PriorityMedium)
	tf.CreateTask("depends on foundation", []string{t1.ID}, PriorityHigh)

	reminder := tf.FormatReminder()
	if reminder == "" {
		t.Fatal("expected non-empty reminder")
	}
	// foundation should appear before dependent task
	foundIdx := -1
	depIdx := -1
	for i, line := range splitLines(reminder) {
		if contains(line, "foundation") && !contains(line, "depends") {
			foundIdx = i
		}
		if contains(line, "depends on foundation") {
			depIdx = i
		}
	}
	if foundIdx == -1 || depIdx == -1 {
		t.Fatalf("could not find tasks in reminder:\n%s", reminder)
	}
	if foundIdx >= depIdx {
		t.Errorf("foundation (line %d) should appear before dependent (line %d)", foundIdx, depIdx)
	}
}

func TestEmptyReadReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	tasks, err := tf.ReadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestFormatReminderEmptyReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	tf := NewTodoFile(dir)

	if r := tf.FormatReminder(); r != "" {
		t.Errorf("expected empty reminder, got %q", r)
	}
}

type eventCollector struct {
	mu   sync.Mutex
	evts []events.Event
}

func (c *eventCollector) Emit(ev events.Event) {
	c.mu.Lock()
	c.evts = append(c.evts, ev)
	c.mu.Unlock()
}

func (c *eventCollector) byType(et events.EventType) []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []events.Event
	for _, ev := range c.evts {
		if ev.Type() == et {
			out = append(out, ev)
		}
	}
	return out
}

func TestEmitsTaskCreatedEvent(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()
	tf := NewTodoFile(dir)
	tf.SetEmitter(collector)

	tf.CreateTask("new task", nil, PriorityMedium)

	created := collector.byType(events.EventTaskCreated)
	if len(created) != 1 {
		t.Fatalf("expected 1 TaskCreated, got %d", len(created))
	}
}

func TestEmitsTaskCompletedEvent(t *testing.T) {
	collector := &eventCollector{}
	dir := t.TempDir()
	tf := NewTodoFile(dir)
	tf.SetEmitter(collector)

	task, _ := tf.CreateTask("task", nil, PriorityMedium)
	tf.UpdateTask(task.ID, "done", "")

	completed := collector.byType(events.EventTaskCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 TaskCompleted, got %d", len(completed))
	}
}

// helpers
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
