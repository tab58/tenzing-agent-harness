package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLsTool(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("12345"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o644)

	ls := &LsTool{}

	t.Run("sorted listing with type and size", func(t *testing.T) {
		res := execTool(t, ls, dir, map[string]any{"path": dir})
		if res.IsError {
			t.Fatalf("ls error: %q", res.Output)
		}
		lines := strings.Split(res.Output, "\n")
		// header + a.txt, b.txt, subdir/ in sorted order
		want := []string{"a.txt  1B", "b.txt  5B", "subdir/"}
		if len(lines) != len(want)+1 {
			t.Fatalf("lines = %q, want header + %d entries", lines, len(want))
		}
		for i, w := range want {
			if lines[i+1] != w {
				t.Errorf("line %d = %q, want %q", i+1, lines[i+1], w)
			}
		}
	})

	t.Run("default path is working dir", func(t *testing.T) {
		res := execTool(t, ls, dir, map[string]any{})
		if res.IsError || !strings.Contains(res.Output, "a.txt") {
			t.Fatalf("default-path ls failed: %v %q", res.IsError, res.Output)
		}
	})

	t.Run("nonexistent path is tool error", func(t *testing.T) {
		res := execTool(t, ls, dir, map[string]any{"path": filepath.Join(dir, "nope")})
		if !res.IsError {
			t.Fatal("ls on missing dir should return tool error")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		empty := filepath.Join(dir, "subdir")
		res := execTool(t, ls, dir, map[string]any{"path": empty})
		if res.IsError || !strings.Contains(res.Output, "empty directory") {
			t.Fatalf("empty-dir ls = %v %q", res.IsError, res.Output)
		}
	})
}
