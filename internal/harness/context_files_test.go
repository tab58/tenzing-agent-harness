package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContextFiles(t *testing.T) {
	home := redirectHome(t)

	// global file under the redirected config dir
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir")
	}
	if !strings.HasPrefix(configDir, home) {
		t.Skipf("config dir %q not under redirected home %q", configDir, home)
	}
	globalDir := filepath.Join(configDir, "tenzing")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "AGENTS.md"), []byte("GLOBAL-RULES"), 0o644)

	// project tree: root AGENTS.md + nested AGENTS.md
	root := t.TempDir()
	sub := filepath.Join(root, "nested")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("ROOT-RULES"), 0o644)
	os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("NESTED-RULES"), 0o644)

	out := loadContextFiles(sub)

	for _, want := range []string{"GLOBAL-RULES", "ROOT-RULES", "NESTED-RULES"} {
		if !strings.Contains(out, want) {
			t.Errorf("context files missing %q", want)
		}
	}
	// order: global, then root→cwd
	gi := strings.Index(out, "GLOBAL-RULES")
	ri := strings.Index(out, "ROOT-RULES")
	ni := strings.Index(out, "NESTED-RULES")
	if !(gi < ri && ri < ni) {
		t.Errorf("order wrong: global=%d root=%d nested=%d", gi, ri, ni)
	}
	// path headers present
	if !strings.Contains(out, filepath.Join(root, "AGENTS.md")) {
		t.Error("missing path header for root AGENTS.md")
	}
}

func TestLoadContextFilesTruncation(t *testing.T) {
	redirectHome(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(strings.Repeat("x", contextFilesMaxBytes+1000)), 0o644)

	out := loadContextFiles(root)
	if !strings.Contains(out, "[context files truncated at 32KB]") {
		t.Error("oversize context files not truncated with marker")
	}
	if len(out) > contextFilesMaxBytes+200 {
		t.Errorf("output len = %d, want capped near %d", len(out), contextFilesMaxBytes)
	}
}

func TestLoadContextFilesEmpty(t *testing.T) {
	redirectHome(t)
	if out := loadContextFiles(t.TempDir()); out != "" {
		t.Errorf("no AGENTS.md anywhere should produce empty string, got %q", out)
	}
}
