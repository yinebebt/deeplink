package deeplink

import (
	"context"
	"maps"
	"sort"
	"sync"
	"time"
)

// MemoryStore implements [Store] using in-memory maps.
// Useful for tests, examples, and local development.
type MemoryStore struct {
	mu        sync.RWMutex
	payload   map[string]*Link
	clicks    map[string]int64
	deletedAt map[string]time.Time
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		payload:   make(map[string]*Link),
		clicks:    make(map[string]int64),
		deletedAt: make(map[string]time.Time),
	}
}

func (s *MemoryStore) Save(_ context.Context, id string, payload *Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := cloneLink(payload)
	cloned.ShortID = id
	s.payload[id] = cloned
	delete(s.deletedAt, id)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sweepLocked(id) {
		return nil, ErrNotFound
	}

	payload, ok := s.payload[id]
	if !ok {
		return nil, ErrNotFound
	}

	return cloneLink(payload), nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sweepLocked(id) {
		return ErrNotFound
	}

	payload, ok := s.payload[id]
	if !ok || payload.DeletedAt != "" {
		return ErrNotFound
	}

	now := time.Now().UTC()
	payload.DeletedAt = now.Format(time.RFC3339)
	s.deletedAt[id] = now
	return nil
}

// sweepLocked removes id when its grace window has elapsed. The caller
// must hold s.mu (write lock). Returns true when the record was purged.
func (s *MemoryStore) sweepLocked(id string) bool {
	t, ok := s.deletedAt[id]
	if !ok {
		return false
	}
	if time.Since(t) < DeleteGracePeriod {
		return false
	}
	delete(s.payload, id)
	delete(s.clicks, id)
	delete(s.deletedAt, id)
	return true
}

// sweepAllLocked snapshots the deleted-IDs map before sweeping so we
// don't mutate the map we're iterating. Caller must hold s.mu.
func (s *MemoryStore) sweepAllLocked() {
	if len(s.deletedAt) == 0 {
		return
	}
	ids := make([]string, 0, len(s.deletedAt))
	for id := range s.deletedAt {
		ids = append(ids, id)
	}
	for _, id := range ids {
		s.sweepLocked(id)
	}
}

func (s *MemoryStore) IncrClick(_ context.Context, id string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clicks[id]++
	return s.clicks[id], nil
}

func (s *MemoryStore) Clicks(_ context.Context, id string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.clicks[id], nil
}

func (s *MemoryStore) List(_ context.Context, linkType string, cursor uint64, count int64) ([]LinkResponse, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count <= 0 {
		count = 100
	}

	s.sweepAllLocked()

	ids := make([]string, 0, len(s.payload))
	for id, payload := range s.payload {
		if payload.Type == linkType && payload.DeletedAt == "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	if cursor >= uint64(len(ids)) {
		return []LinkResponse{}, 0, nil
	}

	start := int(cursor)
	end := min(start+int(count), len(ids))

	links := make([]LinkResponse, 0, end-start)
	for _, id := range ids[start:end] {
		links = append(links, LinkResponse{
			Link:      cloneLink(s.payload[id]),
			ShortLink: id,
			Clicks:    s.clicks[id],
		})
	}

	var next uint64
	if end < len(ids) {
		next = uint64(end)
	}

	return links, next, nil
}

func cloneLink(payload *Link) *Link {
	if payload == nil {
		return nil
	}

	cloned := *payload
	if payload.Metadata != nil {
		cloned.Metadata = make(map[string]any, len(payload.Metadata))
		maps.Copy(cloned.Metadata, payload.Metadata)
	}

	return &cloned
}
