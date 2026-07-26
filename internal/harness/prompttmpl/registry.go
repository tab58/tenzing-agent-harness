// Package prompttmpl implements markdown prompt templates invoked as
// "/name args..." slash commands in a query. Templates are *.md files with
// optional YAML frontmatter (name, description, argument-hint); discovery
// records metadata at registration, bodies load lazily at expansion time.
package prompttmpl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Template is a discovered prompt template.
type Template struct {
	Name         string
	Description  string
	ArgumentHint string
	path         string
}

// Registry maps template names to their files. Later-registered directories
// override earlier ones on name collision (project dir wins over global).
type Registry struct {
	templates map[string]Template
}

func NewRegistry() *Registry {
	return &Registry{templates: make(map[string]Template)}
}

// AddDir scans a directory for *.md templates. Nonexistent or unreadable
// directories are skipped silently, matching the skills registry.
func (r *Registry) AddDir(dir string) {
	entries, err := os.ReadDir(expandTilde(dir))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(expandTilde(dir), entry.Name())
		tmpl := Template{
			Name: strings.TrimSuffix(entry.Name(), ".md"),
			path: path,
		}
		if name, desc, hint, err := parseFrontmatter(path); err == nil {
			if name != "" {
				tmpl.Name = name
			}
			tmpl.Description = desc
			tmpl.ArgumentHint = hint
		}
		r.templates[tmpl.Name] = tmpl
	}
}

// List returns all templates sorted by name.
func (r *Registry) List() []Template {
	out := make([]Template, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Preprocess rewrites a "/name args..." query through the matching template.
// Queries that are not slash commands (no leading "/", or a path-like first
// token containing "/") pass through unchanged. A slash command with no
// matching template returns an error listing the available names.
func (r *Registry) Preprocess(query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	if !strings.HasPrefix(trimmed, "/") {
		return query, nil
	}
	name, args := splitCommand(trimmed[1:])
	if name == "" || !validName(name) {
		return query, nil
	}
	tmpl, ok := r.templates[name]
	if !ok {
		return "", fmt.Errorf("unknown prompt template /%s; available: %s", name, r.available())
	}
	body, err := loadBody(tmpl.path)
	if err != nil {
		return "", fmt.Errorf("load prompt template /%s: %w", name, err)
	}
	return Expand(body, args), nil
}

func (r *Registry) available() string {
	if len(r.templates) == 0 {
		return "(none configured)"
	}
	names := make([]string, 0, len(r.templates))
	for _, t := range r.List() {
		names = append(names, "/"+t.Name)
	}
	return strings.Join(names, ", ")
}

// splitCommand cuts "name args..." at the first whitespace rune.
func splitCommand(s string) (name, args string) {
	if i := strings.IndexFunc(s, unicode.IsSpace); i >= 0 {
		return s[:i], strings.TrimSpace(s[i:])
	}
	return s, ""
}

// validName restricts template names to letters, digits, '.', '-', '_' —
// a first token containing '/' (an absolute path) is not a command.
func validName(name string) bool {
	for _, r := range name {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// loadBody reads a template file and strips its frontmatter block.
func loadBody(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	if strings.HasPrefix(content, "---") {
		if end := strings.Index(content[3:], "---"); end != -1 {
			content = content[end+6:]
		}
	}
	return strings.TrimSpace(content), nil
}

// parseFrontmatter extracts name/description/argument-hint from an optional
// leading YAML frontmatter block. A file without frontmatter is a valid
// template (all fields empty, no error unless unreadable).
func parseFrontmatter(path string) (name, description, argumentHint string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return "", "", "", nil
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return "", "", "", nil
	}
	for _, line := range strings.Split(content[3:end+3], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(k) {
			case "name":
				name = strings.TrimSpace(v)
			case "description":
				description = strings.TrimSpace(v)
			case "argument-hint":
				argumentHint = strings.TrimSpace(v)
			}
		}
	}
	return name, description, argumentHint, nil
}

// expandTilde resolves a leading "~/" against the user's home directory.
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
