package session

import (
	"os"
	"strings"
	"testing"
)

func TestListDeleteRename(t *testing.T) {
	dir := t.TempDir()

	s1 := newTestStore(t, dir, "conv-a")
	s1.Append(Entry{Type: TypeUser, Text: "hello a"})
	s1.Close()
	s2 := newTestStore(t, dir, "conv-b")
	s2.Append(Entry{Type: TypeUser, Text: "hello b"})
	s2.Append(Entry{Type: TypeAssistant, Text: "hi", Model: "m"})
	s2.Close()

	t.Run("list returns both with metadata", func(t *testing.T) {
		infos, err := List(dir, "/proj")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(infos) != 2 {
			t.Fatalf("sessions = %d, want 2", len(infos))
		}
		byID := map[string]Info{}
		for _, in := range infos {
			byID[in.ConversationID] = in
		}
		if byID["conv-a"].Entries != 1 || byID["conv-b"].Entries != 2 {
			t.Errorf("entry counts wrong: %+v", byID)
		}
		if byID["conv-a"].Model != "test-model" {
			t.Errorf("model = %q", byID["conv-a"].Model)
		}
	})

	t.Run("list for other cwd is empty", func(t *testing.T) {
		infos, err := List(dir, "/other")
		if err != nil || len(infos) != 0 {
			t.Fatalf("List other cwd = %v, %v; want empty", infos, err)
		}
	})

	t.Run("rename persists and load skips label", func(t *testing.T) {
		if err := Rename(dir, "/proj", "conv-a", "my session"); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		infos, _ := List(dir, "/proj")
		var found bool
		for _, in := range infos {
			if in.ConversationID == "conv-a" && in.Name == "my session" {
				found = true
			}
		}
		if !found {
			t.Errorf("renamed session not listed with name: %+v", infos)
		}
		// label entries are metadata, not conversation history
		res, err := Load(dir, "/proj", "conv-a")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		for _, m := range res.History {
			if strings.Contains(m.Content[0].Text, "my session") {
				t.Error("label entry leaked into reconstructed history")
			}
		}
	})

	t.Run("rename unknown conversation fails", func(t *testing.T) {
		if err := Rename(dir, "/proj", "nope", "x"); err == nil {
			t.Fatal("Rename of missing session should fail")
		}
	})

	t.Run("delete removes file", func(t *testing.T) {
		var path string
		for _, in := range mustList(t, dir) {
			if in.ConversationID == "conv-b" {
				path = in.Path
			}
		}
		if err := Delete(dir, "/proj", "conv-b"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("session file still exists after delete")
		}
		if len(mustList(t, dir)) != 1 {
			t.Errorf("list should have 1 session after delete")
		}
	})

	t.Run("delete unknown conversation fails", func(t *testing.T) {
		if err := Delete(dir, "/proj", "nope"); err == nil {
			t.Fatal("Delete of missing session should fail")
		}
	})
}

func mustList(t *testing.T, dir string) []Info {
	t.Helper()
	infos, err := List(dir, "/proj")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return infos
}
