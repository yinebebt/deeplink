package deeplink

import (
	"context"
	"maps"
	"sort"
	"sync"
	"time"
)

// MemoryStore implements [Store] using in-memory maps. Useful for tests,
// examples, and local development. Analytics are kept as aggregates (not raw
// events) so memory stays bounded by dimension cardinality rather than visit
// count.
type MemoryStore struct {
	mu        sync.RWMutex
	payload   map[string]*Link
	clicks    map[string]int64
	deletedAt map[string]time.Time
	stats     map[string]*memStats
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		payload:   make(map[string]*Link),
		clicks:    make(map[string]int64),
		deletedAt: make(map[string]time.Time),
		stats:     make(map[string]*memStats),
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
	if linkExpired(payload, time.Now()) {
		s.evictLocked(id)
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
	s.evictLocked(id)
	return true
}

// evictLocked removes every trace of id. Caller must hold s.mu.
func (s *MemoryStore) evictLocked(id string) {
	delete(s.payload, id)
	delete(s.clicks, id)
	delete(s.deletedAt, id)
	delete(s.stats, id)
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

func (s *MemoryStore) IncrClickBy(_ context.Context, id string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clicks[id] += delta
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

	now := time.Now()
	ids := make([]string, 0, len(s.payload))
	for id, payload := range s.payload {
		if payload.Type == linkType && payload.DeletedAt == "" && !linkExpired(payload, now) {
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

// linkExpired reports whether payload has an ExpiresAt that is at or before
// now. An empty or unparseable ExpiresAt is treated as "never expires".
func linkExpired(payload *Link, now time.Time) bool {
	if payload.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return false
	}
	return !exp.After(now)
}

// memStats holds aggregated breakdowns for one link. Total clicks live in
// MemoryStore.clicks; these maps cover only the dimensional splits.
type memStats struct {
	byPlatform map[string]int64
	byReferrer map[string]int64
	byDay      map[string]int64
}

func newMemStats() *memStats {
	return &memStats{
		byPlatform: make(map[string]int64),
		byReferrer: make(map[string]int64),
		byDay:      make(map[string]int64),
	}
}

// RecordEvents folds each event into the per-link aggregates. Empty
// dimension values are skipped so breakdowns stay meaningful.
func (s *MemoryStore) RecordEvents(_ context.Context, events []ClickEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ev := range events {
		agg := s.stats[ev.ShortID]
		if agg == nil {
			agg = newMemStats()
			s.stats[ev.ShortID] = agg
		}
		incrNonEmpty(agg.byPlatform, ev.Platform)
		incrNonEmpty(agg.byReferrer, ev.Referrer)
		agg.byDay[ev.Time.UTC().Format(time.DateOnly)]++
	}
	return nil
}

// Stats returns the aggregated breakdowns plus the live total click count.
func (s *MemoryStore) Stats(_ context.Context, shortID string) (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := Stats{ShortID: shortID, Clicks: s.clicks[shortID]}
	if agg, ok := s.stats[shortID]; ok {
		out.ByPlatform = maps.Clone(agg.byPlatform)
		out.ByReferrer = maps.Clone(agg.byReferrer)
		out.ByDay = maps.Clone(agg.byDay)
	}
	return out, nil
}

func incrNonEmpty(m map[string]int64, key string) {
	if key != "" {
		m[key]++
	}
}
