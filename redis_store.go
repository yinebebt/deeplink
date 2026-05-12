package deeplink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore implements [Store] using Redis.
// Link payloads are stored under "dl:{id}" keys.
// Click counts are stored under "dl:{id}:clicks" keys.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore creates a Redis-backed store.
// Keys are prefixed with "dl:" to avoid collisions with other data.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client, prefix: "dl:"}
}

func (s *RedisStore) key(id string) string          { return s.prefix + id }
func (s *RedisStore) clicksKey(id string) string    { return s.prefix + id + ":clicks" }
func (s *RedisStore) scanPattern() string           { return s.prefix + "*" }
func (s *RedisStore) stripPrefix(key string) string { return strings.TrimPrefix(key, s.prefix) }

func (s *RedisStore) Save(ctx context.Context, id string, payload *Link) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	// SET without an expiration clears any prior TTL on the key, so a
	// re-save during the soft-delete grace window resurrects the link
	// without needing a separate PERSIST call.
	if err := s.client.Set(ctx, s.key(id), string(data), 0).Err(); err != nil {
		return fmt.Errorf("save payload: %w", err)
	}
	return nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	payload, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if payload.DeletedAt != "" {
		return ErrNotFound
	}

	payload.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.key(id), string(data), DeleteGracePeriod)
	pipe.Expire(ctx, s.clicksKey(id), DeleteGracePeriod)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("soft-delete payload: %w", err)
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, id string) (*Link, error) {
	data, err := s.client.Get(ctx, s.key(id)).Result()
	if errors.Is(err, redis.Nil) || len(data) == 0 {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get from redis: %w", err)
	}

	var payload Link
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.ShortID == "" {
		payload.ShortID = id
	}
	return &payload, nil
}

func (s *RedisStore) IncrClick(ctx context.Context, id string) (int64, error) {
	return s.client.Incr(ctx, s.clicksKey(id)).Result()
}

func (s *RedisStore) Clicks(ctx context.Context, id string) (int64, error) {
	n, err := s.client.Get(ctx, s.clicksKey(id)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

func (s *RedisStore) List(ctx context.Context, linkType string, cursor uint64, count int64) ([]LinkResponse, uint64, error) {
	scanCount := max(count, 100)

	keys, nextCursor, err := s.client.Scan(ctx, cursor, s.scanPattern(), scanCount).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("scan redis: %w", err)
	}

	if len(keys) == 0 {
		return nil, nextCursor, nil
	}

	// Filter to payload keys only (skip :clicks keys)
	payloadKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasSuffix(key, ":clicks") {
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

	var links []LinkResponse
	for key, cmd := range cmds {
		val, err := cmd.Result()
		if err != nil {
			continue
		}

		var p Link
		if err := json.Unmarshal([]byte(val), &p); err != nil {
			continue
		}

		if p.Type == linkType && p.DeletedAt == "" {
			clicks, _ := clickCmds[key].Int64()
			id := s.stripPrefix(key)
			if p.ShortID == "" {
				p.ShortID = id
			}
			links = append(links, LinkResponse{
				Link:      &p,
				ShortLink: id,
				Clicks:    clicks,
			})
		}
	}

	return links, nextCursor, nil
}
