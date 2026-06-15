package tooldef

import (
	"crypto/sha256"
	"errors"
	"sync"
)

// Read-before-edit errors returned by FileTracker.Verify. The messages are
// written for the model: they say exactly which action clears the failure.
var (
	ErrNotRead = errors.New(
		"file has not been read in this session; use Read on it first")
	ErrChangedSinceRead = errors.New(
		"file has changed since it was last read; Read it again before editing")
)

// fileStamp identifies the exact content the session last saw for a file.
// Content hash rather than mtime: mtime misses sub-second rewrites and
// mtime-preserving tools.
type fileStamp struct {
	size int64
	hash [sha256.Size]byte
}

func stampOf(content []byte) fileStamp {
	return fileStamp{size: int64(len(content)), hash: sha256.Sum256(content)}
}

// FileTracker enforces the read-before-edit contract for one session: Edit
// and overwriting Write require the file to have been read (or written by a
// tool) this session and unchanged since. This is a freshness guarantee, not
// a full-knowledge one — a truncated Read still stamps the whole file.
//
// Like pathLocks, the map is session-scoped and unbounded; entries are tiny.
type FileTracker struct {
	mu   sync.Mutex
	seen map[string]fileStamp // resolved path → stamp of last-seen content
}

func NewFileTracker() *FileTracker {
	return &FileTracker{seen: make(map[string]fileStamp)}
}

// Record stamps the content the session just saw (Read) or produced
// (Write/Edit) for a resolved path.
func (t *FileTracker) Record(path string, content []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen[path] = stampOf(content)
}

// Verify checks that current matches the last recorded content for the path.
// Callers must hold the file's path lock so the verified content is the same
// content the subsequent write is based on.
func (t *FileTracker) Verify(path string, current []byte) error {
	t.mu.Lock()
	stamp, ok := t.seen[path]
	t.mu.Unlock()

	if !ok {
		return ErrNotRead
	}
	if stamp != stampOf(current) {
		return ErrChangedSinceRead
	}
	return nil
}

// Reset forgets all stamps — used when the conversation history is cleared,
// since the model no longer remembers any file contents either.
func (t *FileTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen = make(map[string]fileStamp)
}
