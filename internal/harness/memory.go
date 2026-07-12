package harness

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const memoryAppDir = "tenzing"
const memoryTTL = 7 * 24 * time.Hour

// memoryDirs returns the main-agent (config) and sub-agent (cache) memory
// directories, creating them if needed. Either may be "" (persistence
// disabled for that class) if the base dir is unavailable.
func memoryDirs() (string, string) {
	return ensureMemoryDir(os.UserConfigDir), ensureMemoryDir(os.UserCacheDir)
}

func ensureMemoryDir(base func() (string, error)) string {
	b, err := base()
	if err != nil {
		slog.Warn("agent memory disabled: base dir unavailable", "error", err)
		return ""
	}
	dir := filepath.Join(b, memoryAppDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("agent memory disabled: cannot create dir", "dir", dir, "error", err)
		return ""
	}
	return dir
}

// memoryStore persists compression summaries as per-conversation files:
// .agent_memory-<YYYYMMDD-HHMM>-<AGENT_ID>.md. Main agent files go to the
// config dir (long-lived), sub-agent files to the cache dir (ephemeral,
// write-only). See docs/superpowers/specs/2026-07-11-agent-memory-design.md.
type memoryStore struct {
	mainID    string
	configDir string
	cacheDir  string
	now       func() time.Time

	mu     sync.Mutex
	stamps map[string]string // runnerID -> session timestamp, fixed at first persist
}

func newMemoryStore(mainID string, now func() time.Time) *memoryStore {
	configDir, cacheDir := memoryDirs()
	return &memoryStore{
		mainID:    mainID,
		configDir: configDir,
		cacheDir:  cacheDir,
		now:       now,
		stamps:    map[string]string{},
	}
}

func (m *memoryStore) stamp(runnerID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.stamps[runnerID]
	if !ok {
		s = m.now().Format("20060102-1504")
		m.stamps[runnerID] = s
	}
	return s
}

// persist writes a summary for runnerID. Never returns an error: memory is
// best-effort and must not disturb the agent loop.
func (m *memoryStore) persist(runnerID, summary string) {
	dir := m.cacheDir
	if runnerID == m.mainID {
		dir = m.configDir
	}
	if dir == "" {
		return
	}
	path := filepath.Join(dir, ".agent_memory-"+m.stamp(runnerID)+"-"+runnerID+".md")
	content := fmt.Sprintf("# Agent Memory\nUpdated: %s\n\n%s\n", m.now().Format(time.RFC3339), summary)
	if err := atomicWrite(path, []byte(content)); err != nil {
		slog.Warn("agent memory write failed", "path", path, "error", err)
	}
}

// loadLatest returns the newest persisted summary for a conversation ID,
// or "" when none exists.
func (m *memoryStore) loadLatest(conversationID string) string {
	if m.configDir == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(m.configDir, ".agent_memory-*-"+conversationID+".md"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches) // timestamp sorts lexically
	data, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		slog.Warn("agent memory read failed", "path", matches[len(matches)-1], "error", err)
		return ""
	}
	return string(data)
}

// sweep deletes memory files older than ttl in both dirs, sparing the
// conversation being resumed. Best-effort; errors are logged and ignored.
func (m *memoryStore) sweep(ttl time.Duration, spareID string) {
	cutoff := time.Now().Add(-ttl)
	for _, dir := range []string{m.configDir, m.cacheDir} {
		if dir == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(dir, ".agent_memory-*"))
		for _, f := range matches {
			if spareID != "" && strings.HasSuffix(f, "-"+spareID+".md") {
				continue
			}
			info, err := os.Stat(f)
			if err != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := os.Remove(f); err != nil {
				slog.Warn("agent memory sweep failed", "path", f, "error", err)
			}
		}
	}
}

// atomicWrite prevents a starting session from reading a torn file: write
// to a temp file in the same dir, then rename over the target.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent_memory_tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
