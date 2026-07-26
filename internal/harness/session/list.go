package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Info is the listing metadata for one session file, read from its header
// plus filesystem stats. Name comes from the last label entry ("" when
// never renamed).
type Info struct {
	ConversationID string    `json:"conversation_id"`
	Name           string    `json:"name,omitempty"`
	Model          string    `json:"model"`
	Created        time.Time `json:"created"`
	Modified       time.Time `json:"modified"`
	Size           int64     `json:"size"`
	Entries        int       `json:"entries"`
	Path           string    `json:"path"`
}

// List returns metadata for every session file recorded under dir for cwd,
// newest-modified first. Unreadable or corrupt files are skipped.
func List(dir, cwd string) ([]Info, error) {
	if dir == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, cwdHash(cwd), "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	infos := make([]Info, 0, len(matches))
	for _, path := range matches {
		entries, header, err := readFile(path)
		if err != nil {
			continue
		}
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		info := Info{
			ConversationID: header.ConversationID,
			Model:          header.Model,
			Created:        header.Time,
			Modified:       stat.ModTime(),
			Size:           stat.Size(),
			Entries:        len(entries),
			Path:           path,
		}
		for _, e := range entries {
			if e.Type == TypeLabel {
				info.Name = e.Text
			}
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Modified.After(infos[j].Modified) })
	return infos, nil
}

// Delete removes every session file for conversationID under dir/cwd.
// Deleting a conversation with no files is an error.
func Delete(dir, cwd, conversationID string) error {
	matches, err := filepath.Glob(filepath.Join(dir, cwdHash(cwd), "*_"+conversationID+".jsonl"))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("no session found for conversation %q", conversationID)
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
	}
	return nil
}

// Rename appends a label entry to the conversation's latest session file.
// Safe alongside a live Store: the entry is out-of-band (empty parent_id)
// and O_APPEND writes to a local file don't interleave with the store's.
func Rename(dir, cwd, conversationID, name string) error {
	path := findLatest(dir, cwd, conversationID)
	if path == "" {
		return fmt.Errorf("no session found for conversation %q", conversationID)
	}
	line, err := json.Marshal(Entry{
		ID:   newEntryID(),
		Type: TypeLabel,
		Time: time.Now(),
		Text: name,
	})
	if err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	return nil
}
