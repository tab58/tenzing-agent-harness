package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store appends session entries to one JSONL file per conversation:
// <dir>/<cwd-hash>/<timestamp>_<conversationID>.jsonl. Resuming a
// conversation appends to its latest existing file. All methods are safe
// for concurrent use and never return errors to callers — persistence is
// best-effort (failures are logged and the store goes inert).
type Store struct {
	path string
	cwd  string

	mu     sync.Mutex
	f      *os.File
	lastID string // parent for the next entry
	broken bool   // a write failed; stop trying
	header Header
}

// DefaultDir returns <UserConfigDir>/tenzing/sessions, or "" when the base
// dir is unavailable (persistence disabled).
func DefaultDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		slog.Warn("session persistence disabled: config dir unavailable", "error", err)
		return ""
	}
	return filepath.Join(base, "tenzing", "sessions")
}

// cwdHash is a short stable directory name for the working directory.
func cwdHash(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(sum[:4])
}

// findLatest returns the newest session file for conversationID under dir,
// or "" when none exists. Filenames start with an RFC3339-like UTC stamp,
// so lexical order is chronological.
func findLatest(dir, cwd, conversationID string) string {
	matches, err := filepath.Glob(filepath.Join(dir, cwdHash(cwd), "*_"+conversationID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// NewStore returns a Store for the conversation. When a session file for
// conversationID already exists under dir it is appended to (resume);
// otherwise a new file is created lazily on first append. A dir of ""
// returns an inert store that drops everything.
func NewStore(dir, cwd, conversationID, model string, now func() time.Time) *Store {
	s := &Store{cwd: cwd}
	if dir == "" {
		s.broken = true
		return s
	}
	s.header = Header{
		Type:           TypeHeader,
		Version:        Version,
		ConversationID: conversationID,
		Cwd:            cwd,
		Model:          model,
		Time:           now(),
	}
	if existing := findLatest(dir, cwd, conversationID); existing != "" {
		s.path = existing
	} else {
		s.path = filepath.Join(dir, cwdHash(cwd),
			now().UTC().Format("20060102T150405Z")+"_"+conversationID+".jsonl")
	}
	return s
}

// Path returns the session file path ("" for an inert store).
func (s *Store) Path() string {
	return s.path
}

// Append writes one entry line, assigning its ID and parent linkage.
// Best-effort: on any failure the store logs once and goes inert.
func (s *Store) Append(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return
	}
	if s.f == nil {
		if !s.open() {
			return
		}
	}

	e.ID = newEntryID()
	e.ParentID = s.lastID
	line, err := json.Marshal(e)
	if err != nil {
		s.fail("marshal session entry", err)
		return
	}
	if _, err := s.f.Write(append(line, '\n')); err != nil {
		s.fail("write session entry", err)
		return
	}
	s.lastID = e.ID
}

// open creates the directory and file and writes the header line for new
// files. For resumed files it seeds parent linkage from the last entry.
// Callers must hold s.mu. Returns false (and marks broken) on failure.
func (s *Store) open() bool {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		s.fail("create session dir", err)
		return false
	}

	_, statErr := os.Stat(s.path)
	isNew := os.IsNotExist(statErr)

	if !isNew {
		// Resume: parent the next entry off the file's last entry.
		entries, _, err := readFile(s.path)
		if err != nil {
			s.fail("read session file for resume", err)
			return false
		}
		if len(entries) > 0 {
			s.lastID = entries[len(entries)-1].ID
		}
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		s.fail("open session file", err)
		return false
	}
	s.f = f

	if isNew {
		line, err := json.Marshal(s.header)
		if err == nil {
			_, err = s.f.Write(append(line, '\n'))
		}
		if err != nil {
			s.fail("write session header", err)
			return false
		}
	}
	return true
}

func (s *Store) fail(what string, err error) {
	slog.Warn("session persistence disabled: "+what, "path", s.path, "error", err)
	s.broken = true
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}

// SaveBlob writes raw bytes content-addressed to <session dir>/blobs/<sha256>
// and returns the hash. Best-effort like Append: "" on failure (logged, but
// the store stays usable — a lost blob only degrades resume, not the log).
func (s *Store) SaveBlob(data []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken || s.path == "" {
		return ""
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	dir := filepath.Join(filepath.Dir(s.path), "blobs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("session blob write failed", "error", err)
		return ""
	}
	path := filepath.Join(dir, sha)
	if _, err := os.Stat(path); err == nil {
		return sha // content-addressed: already present
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("session blob write failed", "path", path, "error", err)
		return ""
	}
	return sha
}

// readBlob loads a sidecar blob by hash, relative to the session file path.
func readBlob(sessionPath, sha string) ([]byte, error) {
	return os.ReadFile(filepath.Join(filepath.Dir(sessionPath), "blobs", sha))
}

// Close flushes and closes the session file. Safe to call multiple times.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	s.broken = true
}
