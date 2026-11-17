package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/yinebebt/keyring"
)

// The API key set is stored next to the links, so `deeplink keys rotate`
// reaches every instance on its next request, without a restart.

// blobStore adapts a get/put of one JSON blob to keyring.Store.
// get returns nil when nothing is stored yet.
type blobStore struct {
	get func(context.Context) ([]byte, error)
	put func(context.Context, []byte) error
}

func (s blobStore) Get(ctx context.Context) (keyring.KeySet, error) {
	var ks keyring.KeySet
	data, err := s.get(ctx)
	if err != nil || data == nil {
		return ks, err
	}
	return ks, json.Unmarshal(data, &ks)
}

func (s blobStore) Put(ctx context.Context, ks keyring.KeySet) error {
	data, err := json.Marshal(ks)
	if err != nil {
		return err
	}
	return s.put(ctx, data)
}

func sqlKeyStore(ctx context.Context, db *sql.DB) (keyring.Store, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_keys (
	id   INTEGER PRIMARY KEY CHECK (id = 1),
	data BLOB NOT NULL
)`); err != nil {
		return nil, fmt.Errorf("init api_keys table: %w", err)
	}
	return blobStore{
		get: func(ctx context.Context) ([]byte, error) {
			var data []byte
			err := db.QueryRowContext(ctx, `SELECT data FROM api_keys`).Scan(&data)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return data, err
		},
		put: func(ctx context.Context, data []byte) error {
			_, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO api_keys (id, data) VALUES (1, ?)`, data)
			return err
		},
	}, nil
}

// redisKeysKey sits outside redisstore's "dl:" prefix, which link listing scans.
const redisKeysKey = "deeplink:api_keys"

func redisKeyStore(client *redis.Client) keyring.Store {
	return blobStore{
		get: func(ctx context.Context) ([]byte, error) {
			data, err := client.Get(ctx, redisKeysKey).Bytes()
			if errors.Is(err, redis.Nil) {
				return nil, nil
			}
			return data, err
		},
		put: func(ctx context.Context, data []byte) error {
			return client.Set(ctx, redisKeysKey, data, 0).Err()
		},
	}
}

func memKeyStore() keyring.Store {
	var (
		mu   sync.Mutex
		blob []byte
	)
	return blobStore{
		get: func(context.Context) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			return blob, nil
		},
		put: func(_ context.Context, data []byte) error {
			mu.Lock()
			defer mu.Unlock()
			blob = data
			return nil
		},
	}
}
