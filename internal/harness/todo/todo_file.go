package todo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tenzing-agent/internal/harness/tools/tooldef"
)

const CanonicalTodoFileName = ".agent_todo.json"

type TodoItem struct {
	Index  int    `json:"index"`
	Task   string `json:"task"`
	Status string `json:"status"`
}

type TodoFile struct {
	filename string
	cwd      string
}

func NewTodoItemFile(cwd string) *TodoFile {
	return &TodoFile{
		filename: CanonicalTodoFileName,
		cwd:      cwd,
	}
}

func (f *TodoFile) GetTools() []tooldef.Definition {
	return []tooldef.Definition{
		NewTodoReadTool(f),
		NewTodoWriteTool(f),
		NewTodoUpdateTool(f),
	}
}

func (f *TodoFile) FilePath() string {
	return filepath.Join(f.cwd, CanonicalTodoFileName)
}

func (f *TodoFile) ReadItems() ([]TodoItem, error) {
	data, err := os.ReadFile(f.FilePath())
	if err != nil {
		return nil, fmt.Errorf("no todo plan found — call TodoWrite first: %w", err)
	}
	var items []TodoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("corrupted todo file: %w", err)
	}
	return items, nil
}

func (f *TodoFile) WriteItems(items []TodoItem) error {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal todos: %w", err)
	}
	return os.WriteFile(f.FilePath(), data, 0644)
}

func (f *TodoFile) FormatItems(items []TodoItem) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "[%d] [%s] %s\n", item.Index, item.Status, item.Task)
	}
	return b.String()
}
