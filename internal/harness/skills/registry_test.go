package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: test skill %s\n---\nbody\n", name, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSkipsUnreadableDirs(t *testing.T) {
	good := t.TempDir()
	writeSkill(t, good, "alpha")

	r := NewRegistry()
	r.RegisterSkillDir(filepath.Join(t.TempDir(), "does-not-exist")) // registered first, must not abort the scan
	r.RegisterSkillDir(good)

	if _, err := r.Load("alpha"); err != nil {
		t.Fatalf("skill in later dir not discovered past a bad dir: %v", err)
	}
}

func TestRegisterSkillDirExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSkill(t, filepath.Join(home, "skills"), "beta")

	r := NewRegistry()
	r.RegisterSkillDir("~/skills")

	if _, err := r.Load("beta"); err != nil {
		t.Fatalf("tilde dir not expanded/discovered: %v", err)
	}
}
