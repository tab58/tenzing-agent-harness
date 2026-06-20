package tooldef

import "sync"

type SnapshotStore struct {
	mu        sync.Mutex
	snapshots map[string][]byte
}

func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{snapshots: make(map[string][]byte)}
}

func (s *SnapshotStore) Save(path string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[path] = content
}

func (s *SnapshotStore) Pop(path string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.snapshots[path]
	if ok {
		delete(s.snapshots, path)
	}
	return content, ok
}

func (s *SnapshotStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = make(map[string][]byte)
}
