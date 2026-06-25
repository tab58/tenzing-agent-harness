package skills

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"tenzing-agent/internal/harness/tools/tooldef"
)

type Definition struct {
	Name        string
	Description string
	path        string
}

type Registry struct {
	skills map[string]Definition
	dirs   []string
}

func NewRegistry() *Registry {
	r := &Registry{
		skills: make(map[string]Definition),
		dirs:   make([]string, 0),
	}
	r.discover()
	return r
}

func (r *Registry) RegisterSkillDir(skillDir string) {
	r.dirs = append(r.dirs, skillDir)
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

func (r *Registry) List() map[string]string {
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

func (r *Registry) discover() {
	for _, skillsDir := range r.dirs {
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
