package tooldef

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// pathLocks serializes read-modify-write cycles per resolved path so
// concurrent Edit/Write calls (e.g. from parallel subagents) cannot lose
// updates. Grows with the number of distinct paths touched in-process;
// entries are tiny and processes are session-scoped, so no eviction.
var pathLocks sync.Map // map[string]*sync.Mutex

// lockPath locks the mutex for a resolved path and returns the unlock func:
//
//	defer lockPath(path)()
func lockPath(path string) func() {
	m, _ := pathLocks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so a crash mid-write never leaves a half-written
// file. An existing file keeps its permission bits; new files get 0644.
func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	// Flush to stable storage before the rename makes the file visible, so a
	// crash cannot publish a partially written file.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
