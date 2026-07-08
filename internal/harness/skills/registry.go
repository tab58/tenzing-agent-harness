package skills

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools/tooldef"
)

type Definition struct {
	Name        string
	Description string
	path        string
}

type Registry struct {
	skills map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]Definition),
	}
}

func (r *Registry) RegisterSkillDir(skillDir string) {
	r.discoverDir(expandTilde(skillDir))
}

// expandTilde resolves a leading "~/" against the user's home directory.
// Paths without the prefix are returned unchanged, as is the input when the
// home directory cannot be determined.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return path
}

func (r *Registry) GetTools() []tooldef.Definition {
	return []tooldef.Definition{
		NewLoadSkillTool(r),
		NewListSkillsTool(r),
	}
}

func (r *Registry) Discover() map[string]Definition {
	result := make(map[string]Definition, len(r.skills))
	maps.Copy(result, r.skills)
	return result
}

func (r *Registry) GetSkillMap() map[string]string {
	result := make(map[string]string, len(r.skills))
	for _, def := range r.skills {
		result[def.Name] = def.Description
	}
	return result
}

func (r *Registry) Load(name string) (string, error) {
	def, ok := r.skills[name]
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	data, err := os.ReadFile(def.path)
	if err != nil {
		return "", fmt.Errorf("read skill %q: %w", name, err)
	}
	return fmt.Sprintf("=== SKILL: %s ===\n%s", name, string(data)), nil
}

// discoverDir scans a single skills directory and registers every valid
// skill found. Unreadable directories are skipped silently.
func (r *Registry) discoverDir(skillsDir string) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		name, desc, err := parseFrontmatter(path)
		if err != nil {
			continue
		}
		r.skills[name] = Definition{
			Name:        name,
			Description: desc,
			path:        path,
		}
	}
}

func parseFrontmatter(path string) (name string, description string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return "", "", fmt.Errorf("no frontmatter")
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return "", "", fmt.Errorf("unclosed frontmatter")
	}
	fm := content[3 : end+3]

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(k) {
			case "name":
				name = strings.TrimSpace(v)
			case "description":
				description = strings.TrimSpace(v)
			}
		}
	}
	if name == "" {
		return "", "", fmt.Errorf("missing name")
	}
	return name, description, nil
}
