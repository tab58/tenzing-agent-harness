package prompttmpl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemplate(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRegistryDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "review.md", "---\ndescription: Review code\nargument-hint: <file>\n---\nReview $1 carefully.")
	writeTemplate(t, dir, "plain.md", "No frontmatter body $@")
	writeTemplate(t, dir, "renamed.md", "---\nname: alias\n---\nbody")
	writeTemplate(t, dir, "notes.txt", "ignored")

	r := NewRegistry()
	r.AddDir(dir)

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 templates, got %d: %#v", len(list), list)
	}
	// sorted: alias, plain, review
	if list[0].Name != "alias" || list[1].Name != "plain" || list[2].Name != "review" {
		t.Errorf("unexpected names: %#v", list)
	}
	if list[2].Description != "Review code" || list[2].ArgumentHint != "<file>" {
		t.Errorf("frontmatter not parsed: %#v", list[2])
	}
}

func TestRegistryLaterDirWins(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	writeTemplate(t, global, "review.md", "global body")
	writeTemplate(t, project, "review.md", "project body")

	r := NewRegistry()
	r.AddDir(global)
	r.AddDir(project)

	got, err := r.Preprocess("/review")
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if got != "project body" {
		t.Errorf("expected project template to win, got %q", got)
	}
}

func TestRegistryMissingDirIsSilent(t *testing.T) {
	r := NewRegistry()
	r.AddDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(r.List()) != 0 {
		t.Errorf("expected empty registry")
	}
}

func TestPreprocess(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "review.md", "---\ndescription: d\n---\nReview $1 for ${2:-bugs}.")
	r := NewRegistry()
	r.AddDir(dir)

	tests := []struct {
		name    string
		query   string
		want    string
		wantErr string
	}{
		{"expansion", "/review src/foo.go style", "Review src/foo.go for style.", ""},
		{"default arg", "/review src/foo.go", "Review src/foo.go for bugs.", ""},
		{"passthrough plain", "just a question", "just a question", ""},
		{"passthrough path", "/usr/bin/ls is what?", "/usr/bin/ls is what?", ""},
		{"passthrough bare slash", "/ ", "/ ", ""},
		{"unknown command", "/deploy prod", "", "unknown prompt template /deploy; available: /review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Preprocess(tt.query)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Preprocess(%q): %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("Preprocess(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestPreprocessEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	_, err := r.Preprocess("/anything at all")
	if err == nil || !strings.Contains(err.Error(), "(none configured)") {
		t.Fatalf("expected none-configured error, got %v", err)
	}
}

// Bodies load lazily: deleting the file after discovery surfaces as an
// expansion error, proving discovery did not cache the body.
func TestPreprocessLazyLoad(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "gone.md", "body")
	r := NewRegistry()
	r.AddDir(dir)
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Preprocess("/gone"); err == nil {
		t.Fatal("expected load error after file removal")
	}
}
