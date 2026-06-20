package tooldef

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const todoFileName = ".agent_todo.json"

func ReadTodoReminder(workingDir string) string {
	items, err := readTodoItems(workingDir)
	if err != nil {
		return ""
	}
	return "<system-reminder>\nCurrent plan:\n" + formatTodoItems(items) + "</system-reminder>"
}

func todoFilePath(workingDir string) string {
	return filepath.Join(workingDir, todoFileName)
}

func readTodoItems(workingDir string) ([]TodoItem, error) {
	data, err := os.ReadFile(todoFilePath(workingDir))
	if err != nil {
		return nil, fmt.Errorf("no todo plan found — call TodoWrite first: %w", err)
	}
	var items []TodoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("corrupted todo file: %w", err)
	}
	return items, nil
}

func writeTodoItems(workingDir string, items []TodoItem) error {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal todos: %w", err)
	}
	return os.WriteFile(todoFilePath(workingDir), data, 0644)
}

func formatTodoItems(items []TodoItem) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "[%d] [%s] %s\n", item.Index, item.Status, item.Task)
	}
	return b.String()
}
