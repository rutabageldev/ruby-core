package idempotency

import (
	"sync"
	"time"
)

// memStore is a TTL-based in-memory idempotency store.
// Entries are evicted by a background goroutine every ttl/4.
// The store is safe for concurrent use.
type memStore struct {
	mu      sync.RWMutex
	entries map[string]time.Time // id → expiry
	ttl     time.Duration
	stopCh  chan struct{}
}

func newMemStore(ttl time.Duration) *memStore {
	s := &memStore{
		entries: make(map[string]time.Time),
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
	go s.evictLoop()
	return s
}

// Seen reports whether id has been marked and has not yet expired.
func (s *memStore) Seen(id string) (bool, error) {
	s.mu.RLock()
	expiry, ok := s.entries[id]
	s.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return time.Now().Before(expiry), nil
}

// Mark records id as processed with the configured TTL.
func (s *memStore) Mark(id string) error {
	s.mu.Lock()
	s.entries[id] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return nil
}

// Close stops the background eviction goroutine.
func (s *memStore) Close() error {
	close(s.stopCh)
	return nil
}

// evictLoop periodically removes expired entries.
func (s *memStore) evictLoop() {
	ticker := time.NewTicker(s.ttl / 4)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.evict()
		case <-s.stopCh:
			return
		}
	}
}

func (s *memStore) evict() {
	now := time.Now()
	s.mu.Lock()
	for id, expiry := range s.entries {
		if now.After(expiry) {
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()
}
