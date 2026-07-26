package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// contextFilesMaxBytes caps the total AGENTS.md content appended to the
// system prompt; overflow is truncated with a marker.
const contextFilesMaxBytes = 32 * 1024

// loadContextFiles collects AGENTS.md content for the system prompt:
// the global <UserConfigDir>/tenzing/AGENTS.md first, then every AGENTS.md
// on the ancestor chain from the filesystem root down to cwd (root→cwd
// order, so deeper files override by appearing later). Only AGENTS.md is
// honored — matching Pi — not CLAUDE.md. Returns "" when none exist.
func loadContextFiles(cwd string) string {
	var paths []string
	if base, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(base, "tenzing", "AGENTS.md"))
	}
	paths = append(paths, ancestorChain(cwd)...)

	var sb strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n\n# Context from %s\n\n%s", p, strings.TrimSpace(string(data)))
	}
	out := sb.String()
	if len(out) > contextFilesMaxBytes {
		out = out[:contextFilesMaxBytes] + "\n\n[context files truncated at 32KB]"
	}
	if out == "" {
		return ""
	}
	return "\n\n# Project context files (AGENTS.md)" + out
}

// ancestorChain returns candidate AGENTS.md paths from the filesystem root
// down to cwd, inclusive.
func ancestorChain(cwd string) []string {
	var dirs []string
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, filepath.Join(dir, "AGENTS.md"))
		if dir == filepath.Dir(dir) {
			break
		}
	}
	// reverse: root first, cwd last
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}
