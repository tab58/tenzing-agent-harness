package todo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/tools/tooldef"
)

const TodoFileName = ".agent_todo.json"

type TaskPriority string

const (
	PriorityHigh   TaskPriority = "high"
	PriorityMedium TaskPriority = "medium"
	PriorityLow    TaskPriority = "low"
)

type Task struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Status      string       `json:"status"`
	Priority    TaskPriority `json:"priority"`
	DependsOn   []string     `json:"depends_on"`
	Result      string       `json:"result,omitempty"`
}

type TodoFile struct {
	file    string
	mu      sync.Mutex
	emitter events.Emitter
}

func NewTodoFile(cwd string) *TodoFile {
	return &TodoFile{
		file: filepath.Join(cwd, TodoFileName),
	}
}

func (f *TodoFile) SetEmitter(e events.Emitter) {
	f.emitter = e
}

func (f *TodoFile) GetTools() []tooldef.Definition {
	return []tooldef.Definition{
		NewTodoWriteTool(f),
		NewTodoCreateTool(f),
		NewTodoUpdateTool(f),
		NewTodoNextTool(f),
		NewTodoReadTool(f),
	}
}

func (f *TodoFile) ReadTasks() ([]Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load()
}

func (f *TodoFile) WriteTasks(tasks []Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.save(tasks)
}

func (f *TodoFile) CreateTask(desc string, dependsOn []string, priority TaskPriority) (Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tasks, err := f.load()
	if err != nil {
		return Task{}, err
	}

	if priority == "" {
		priority = PriorityMedium
	}
	if dependsOn == nil {
		dependsOn = []string{}
	}

	for _, dep := range dependsOn {
		if !taskExists(tasks, dep) {
			return Task{}, fmt.Errorf("dependency %q not found", dep)
		}
	}

	task := Task{
		ID:          randomID(8),
		Description: desc,
		Status:      "pending",
		Priority:    priority,
		DependsOn:   dependsOn,
	}

	tasks = append(tasks, task)
	if err := f.save(tasks); err != nil {
		return Task{}, err
	}

	if f.emitter != nil {
		f.emitter.Emit(events.TaskCreatedEvent{
			BaseEvent:   events.NewBaseEvent(events.EventTaskCreated, ""),
			TaskID:      task.ID,
			Description: desc,
		})
	}

	return task, nil
}

func (f *TodoFile) UpdateTask(taskID string, status string, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	tasks, err := f.load()
	if err != nil {
		return err
	}

	found := false
	updated := make([]Task, len(tasks))
	for i, t := range tasks {
		if t.ID == taskID || strings.HasPrefix(t.ID, taskID) {
			updated[i] = Task{
				ID:          t.ID,
				Description: t.Description,
				Status:      status,
				Priority:    t.Priority,
				DependsOn:   t.DependsOn,
				Result:      result,
			}
			found = true
		} else {
			updated[i] = t
		}
	}

	if !found {
		return fmt.Errorf("task %q not found", taskID)
	}

	if err := f.save(updated); err != nil {
		return err
	}

	if f.emitter != nil && status == "done" {
		f.emitter.Emit(events.TaskCompletedEvent{
			BaseEvent: events.NewBaseEvent(events.EventTaskCompleted, ""),
			TaskID:    taskID,
		})
	}

	return nil
}

func (f *TodoFile) NextTask() (Task, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tasks, err := f.load()
	if err != nil {
		return Task{}, false, err
	}

	doneIDs := make(map[string]struct{})
	for _, t := range tasks {
		if t.Status == "done" {
			doneIDs[t.ID] = struct{}{}
		}
	}

	priorityOrder := map[TaskPriority]int{PriorityHigh: 0, PriorityMedium: 1, PriorityLow: 2}

	var pending []Task
	for _, t := range tasks {
		if t.Status != "pending" {
			continue
		}
		if allDepsDone(t.DependsOn, doneIDs) {
			pending = append(pending, t)
		}
	}

	sort.SliceStable(pending, func(i, j int) bool {
		return priorityOrder[pending[i].Priority] < priorityOrder[pending[j].Priority]
	})

	if len(pending) == 0 {
		return Task{}, false, nil
	}

	return pending[0], true, nil
}

func (f *TodoFile) FormatReminder() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	tasks, err := f.load()
	if err != nil || len(tasks) == 0 {
		return ""
	}

	sorted := topoSort(tasks)

	var b strings.Builder
	b.WriteString("<system-reminder>\nCurrent plan:\n")
	for _, t := range sorted {
		fmt.Fprintf(&b, "[%s] [%s] %s", abbreviateID(t.ID), t.Status, t.Description)
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&b, " (depends: %s)", strings.Join(abbreviateIDs(t.DependsOn), ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("</system-reminder>")
	return b.String()
}

// --- internal helpers ---

func (f *TodoFile) load() ([]Task, error) {
	data, err := os.ReadFile(f.file)
	if os.IsNotExist(err) {
		return []Task{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tasks: %w", err)
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse tasks: %w", err)
	}
	return tasks, nil
}

func (f *TodoFile) save(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	return os.WriteFile(f.file, data, 0644)
}

func taskExists(tasks []Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id || strings.HasPrefix(t.ID, id) {
			return true
		}
	}
	return false
}

func allDepsDone(deps []string, doneIDs map[string]struct{}) bool {
	for _, dep := range deps {
		if _, ok := doneIDs[dep]; !ok {
			return false
		}
	}
	return true
}

func topoSort(tasks []Task) []Task {
	idIndex := make(map[string]int, len(tasks))
	for i, t := range tasks {
		idIndex[t.ID] = i
	}

	visited := make(map[string]bool, len(tasks))
	var result []Task
	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		idx, ok := idIndex[id]
		if !ok {
			return
		}
		for _, dep := range tasks[idx].DependsOn {
			visit(dep)
		}
		result = append(result, tasks[idx])
	}

	for _, t := range tasks {
		visit(t.ID)
	}
	return result
}

func abbreviateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func abbreviateIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = abbreviateID(id)
	}
	return out
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
