// Package redisstore provides a Redis-backed [deeplink.Store], including the
// click-analytics breakdowns. Importing it pulls in go-redis; the core
// deeplink package does not.
package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yinebebt/deeplink"
)

var _ deeplink.Store = (*Store)(nil)

// Store implements [deeplink.Store] using Redis:
//   - "dl:{id}"        link payload (JSON)
//   - "dl:{id}:clicks" click counter
//   - "dl:{id}:stats"  breakdown hash, fields "<dim>:<value>" (HINCRBY)
//
// All three keys share the link's TTL and soft-delete grace.
type Store struct {
	client *redis.Client
	prefix string
}

// New creates a Redis-backed store. Keys are prefixed with "dl:" to avoid
// collisions with other data.
func New(client *redis.Client) *Store {
	return &Store{client: client, prefix: "dl:"}
}

func (s *Store) key(id string) string          { return s.prefix + id }
func (s *Store) clicksKey(id string) string    { return s.prefix + id + ":clicks" }
func (s *Store) statsKey(id string) string     { return s.prefix + id + ":stats" }
func (s *Store) scanPattern() string           { return s.prefix + "*" }
func (s *Store) stripPrefix(key string) string { return strings.TrimPrefix(key, s.prefix) }

func (s *Store) Save(ctx context.Context, id string, payload *deeplink.Link) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	// A zero expiration clears any prior TTL on the key, so a re-save during
	// the soft-delete grace window resurrects the link without a separate
	// PERSIST call. A future ExpiresAt sets a native TTL: Redis evicts the
	// key on expiry, after which Get returns ErrNotFound for free.
	expiration := payloadTTL(payload)
	if err := s.client.Set(ctx, s.key(id), string(data), expiration).Err(); err != nil {
		return fmt.Errorf("save payload: %w", err)
	}
	// Keep the side keys' TTL in lock-step with the link: match a finite
	// expiry, or clear a leftover grace TTL on resurrect.
	for _, k := range []string{s.clicksKey(id), s.statsKey(id)} {
		if expiration > 0 {
			s.client.Expire(ctx, k, expiration)
		} else {
			s.client.Persist(ctx, k)
		}
	}
	return nil
}

// payloadTTL returns the time until a link's ExpiresAt, or 0 if it has none
// or has already passed.
func payloadTTL(payload *deeplink.Link) time.Duration {
	if payload.ExpiresAt == "" {
		return 0
	}
	exp, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return 0
	}
	d := time.Until(exp)
	if d <= 0 {
		return 0
	}
	return d
}

func (s *Store) Delete(ctx context.Context, id string) error {
	payload, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if payload.DeletedAt != "" {
		return deeplink.ErrNotFound
	}

	payload.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.key(id), string(data), deeplink.DeleteGracePeriod)
	pipe.Expire(ctx, s.clicksKey(id), deeplink.DeleteGracePeriod)
	pipe.Expire(ctx, s.statsKey(id), deeplink.DeleteGracePeriod)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("soft-delete payload: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*deeplink.Link, error) {
	data, err := s.client.Get(ctx, s.key(id)).Result()
	if errors.Is(err, redis.Nil) || len(data) == 0 {
		return nil, deeplink.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get from redis: %w", err)
	}

	var payload deeplink.Link
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.ShortID == "" {
		payload.ShortID = id
	}
	return &payload, nil
}

func (s *Store) IncrClickBy(ctx context.Context, id string, delta int64) (int64, error) {
	return s.client.IncrBy(ctx, s.clicksKey(id), delta).Result()
}

func (s *Store) Clicks(ctx context.Context, id string) (int64, error) {
	n, err := s.client.Get(ctx, s.clicksKey(id)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

func (s *Store) List(ctx context.Context, linkType string, cursor uint64, count int64) ([]deeplink.LinkResponse, uint64, error) {
	scanCount := max(count, 100)

	keys, nextCursor, err := s.client.Scan(ctx, cursor, s.scanPattern(), scanCount).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("scan redis: %w", err)
	}

	if len(keys) == 0 {
		return nil, nextCursor, nil
	}

	// Filter to payload keys only (skip the :clicks and :stats side keys).
	payloadKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasSuffix(key, ":clicks") && !strings.HasSuffix(key, ":stats") {
			payloadKeys = append(payloadKeys, key)
		}
	}

	if len(payloadKeys) == 0 {
		return nil, nextCursor, nil
	}

	pipe := s.client.Pipeline()
	cmds := make(map[string]*redis.StringCmd, len(payloadKeys))
	clickCmds := make(map[string]*redis.StringCmd, len(payloadKeys))

	for _, key := range payloadKeys {
		cmds[key] = pipe.Get(ctx, key)
		clickCmds[key] = pipe.Get(ctx, key+":clicks")
	}

	_, err = pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, 0, fmt.Errorf("exec pipeline: %w", err)
	}

	var links []deeplink.LinkResponse
	for key, cmd := range cmds {
		val, err := cmd.Result()
		if err != nil {
			continue
		}

		var p deeplink.Link
		if err := json.Unmarshal([]byte(val), &p); err != nil {
			continue
		}

		if p.Type == linkType && p.DeletedAt == "" {
			clicks, _ := clickCmds[key].Int64()
			id := s.stripPrefix(key)
			if p.ShortID == "" {
				p.ShortID = id
			}
			links = append(links, deeplink.LinkResponse{
				Link:      &p,
				ShortLink: id,
				Clicks:    clicks,
			})
		}
	}

	return links, nextCursor, nil
}

// RecordEvents increments the per-link stats hash with HINCRBY, one field
// per "<dimension>:<value>", in a single pipeline. Empty dimensions skipped.
func (s *Store) RecordEvents(ctx context.Context, events []deeplink.ClickEvent) error {
	if len(events) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for _, ev := range events {
		k := s.statsKey(ev.ShortID)
		if ev.Platform != "" {
			pipe.HIncrBy(ctx, k, "platform:"+ev.Platform, 1)
		}
		if ev.Referrer != "" {
			pipe.HIncrBy(ctx, k, "referrer:"+ev.Referrer, 1)
		}
		pipe.HIncrBy(ctx, k, "day:"+ev.Time.UTC().Format(time.DateOnly), 1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record events: %w", err)
	}
	return nil
}

// Stats reads the total click counter plus the stats hash, routing each
// "<dimension>:<value>" field into the matching breakdown map.
func (s *Store) Stats(ctx context.Context, shortID string) (deeplink.Stats, error) {
	out := deeplink.Stats{ShortID: shortID}

	clicks, err := s.Clicks(ctx, shortID)
	if err != nil {
		return out, err
	}
	out.Clicks = clicks

	fields, err := s.client.HGetAll(ctx, s.statsKey(shortID)).Result()
	if err != nil {
		return out, fmt.Errorf("get stats: %w", err)
	}

	for field, val := range fields {
		dim, key, ok := strings.Cut(field, ":")
		if !ok || key == "" {
			continue
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			continue
		}
		switch dim {
		case "platform":
			putStat(&out.ByPlatform, key, n)
		case "referrer":
			putStat(&out.ByReferrer, key, n)
		case "day":
			putStat(&out.ByDay, key, n)
		}
	}
	return out, nil
}

// putStat lazily inits the map so empty breakdowns stay nil (omitted from JSON).
func putStat(m *map[string]int64, key string, n int64) {
	if *m == nil {
		*m = make(map[string]int64)
	}
	(*m)[key] = n
}
