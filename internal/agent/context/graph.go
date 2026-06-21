package context

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
)

const TasksFileName = ".agent_tasks.json"

type TaskPriority string

const (
	TaskPriorityHigh   TaskPriority = "high"
	TaskPriorityMedium TaskPriority = "medium"
	TaskPriorityLow    TaskPriority = "low"
)

type Task struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Status      string       `json:"status"`
	Priority    TaskPriority `json:"priority"`
	DependsOn   []string     `json:"depends_on"`
	Result      string       `json:"result,omitempty"`
}

type TaskGraph struct {
	file string
	mu   sync.Mutex
}

func NewTaskGraph(cwd string) *TaskGraph {
	return &TaskGraph{
		file: filepath.Join(cwd, TasksFileName),
	}
}

func (g *TaskGraph) CreateTask(desc string, dependsOn []string, priority TaskPriority) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.load()
	if err != nil {
		return "", err
	}

	if priority == "" {
		priority = TaskPriorityMedium
	}

	if dependsOn == nil {
		dependsOn = []string{}
	}

	for _, dep := range dependsOn {
		if !taskExists(tasks, dep) {
			return "", fmt.Errorf("dependency %q not found", dep)
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
	if err := g.save(tasks); err != nil {
		return "", err
	}

	data, _ := json.Marshal(task)
	return string(data), nil
}

func (g *TaskGraph) NextTask() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.load()
	if err != nil {
		return "", err
	}

	doneIDs := make(map[string]struct{})
	for _, t := range tasks {
		if t.Status == "done" {
			doneIDs[t.ID] = struct{}{}
		}
	}

	priorityOrder := map[TaskPriority]int{TaskPriorityHigh: 0, TaskPriorityMedium: 1, TaskPriorityLow: 2}

	var pending []Task
	for _, t := range tasks {
		if t.Status != "pending" {
			continue
		}
		if allDepsSatisfied(t.DependsOn, doneIDs) {
			pending = append(pending, t)
		}
	}

	sort.SliceStable(pending, func(i, j int) bool {
		return priorityOrder[pending[i].Priority] < priorityOrder[pending[j].Priority]
	})

	if len(pending) == 0 {
		return "", nil
	}

	data, _ := json.Marshal(pending[0])
	return string(data), nil
}

func (g *TaskGraph) UpdateTask(taskID string, status string, result string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.load()
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
	return g.save(updated)
}

func (g *TaskGraph) ListTasks() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.load()
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "[]", nil
	}

	data, _ := json.MarshalIndent(tasks, "", "  ")
	return string(data), nil
}

func (g *TaskGraph) Reminder() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.load()
	if err != nil || len(tasks) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<system-reminder>\nTask graph:\n")
	for _, t := range tasks {
		fmt.Fprintf(&b, "[%s] [%s] %s", t.ID[:6], t.Status, t.Description)
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&b, " (depends: %s)", strings.Join(t.DependsOn, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("</system-reminder>")
	return b.String()
}

func (g *TaskGraph) load() ([]Task, error) {
	data, err := os.ReadFile(g.file)
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

func (g *TaskGraph) save(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	return os.WriteFile(g.file, data, 0644)
}

func taskExists(tasks []Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id || strings.HasPrefix(t.ID, id) {
			return true
		}
	}
	return false
}

func allDepsSatisfied(deps []string, doneIDs map[string]struct{}) bool {
	for _, dep := range deps {
		if _, ok := doneIDs[dep]; !ok {
			return false
		}
	}
	return true
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
