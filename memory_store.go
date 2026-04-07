package deeplink

import (
	"context"
	"maps"
	"sort"
	"sync"
)

// MemoryStore implements [Store] using in-memory maps.
// Useful for tests, examples, and local development.
type MemoryStore struct {
	mu      sync.RWMutex
	payload map[string]*Link
	clicks  map[string]int64
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		payload: make(map[string]*Link),
		clicks:  make(map[string]int64),
	}
}

func (s *MemoryStore) Save(_ context.Context, id string, payload *Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := cloneLink(payload)
	cloned.ShortID = id
	s.payload[id] = cloned
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	payload, ok := s.payload[id]
	if !ok {
		return nil, ErrNotFound
	}

	return cloneLink(payload), nil
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

func (s *MemoryStore) List(_ context.Context, linkType string, cursor uint64, count int64) ([]LinkInfo, uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count <= 0 {
		count = 100
	}

	ids := make([]string, 0, len(s.payload))
	for id, payload := range s.payload {
		if payload.Type == linkType {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	if cursor >= uint64(len(ids)) {
		return []LinkInfo{}, 0, nil
	}

	start := int(cursor)
	end := min(start+int(count), len(ids))

	links := make([]LinkInfo, 0, end-start)
	for _, id := range ids[start:end] {
		payload := s.payload[id]
		links = append(links, LinkInfo{
			ShortLink: id,
			URL:       payload.URL,
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
