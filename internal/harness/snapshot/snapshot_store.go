package snapshot

import "sync"

type Store struct {
	mu        sync.Mutex
	snapshots map[string][]byte
}

func NewSnapshotStore() *Store {
	return &Store{snapshots: make(map[string][]byte)}
}

func (s *Store) Save(path string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[path] = content
}

func (s *Store) Pop(path string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.snapshots[path]
	if ok {
		delete(s.snapshots, path)
	}
	return content, ok
}

func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = make(map[string][]byte)
}
