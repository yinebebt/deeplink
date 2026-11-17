package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yinebebt/keyring"
)

func sqliteKeys(t *testing.T, path string) keyring.Store {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := sqlKeyStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestKeyStores(t *testing.T) {
	stores := map[string]keyring.Store{
		"sqlite": sqliteKeys(t, filepath.Join(t.TempDir(), "deeplink.db")),
		"memory": memKeyStore(),
	}
	if addr := os.Getenv("DEEPLINK_TEST_REDIS"); addr != "" {
		client := redis.NewClient(&redis.Options{Addr: addr})
		client.Del(context.Background(), redisKeysKey)
		t.Cleanup(func() { client.Del(context.Background(), redisKeysKey); _ = client.Close() })
		stores["redis"] = redisKeyStore(client)
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			kr := keyring.New(store, time.Hour)
			first, _ := kr.Rotate(ctx)
			second, err := kr.Rotate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := kr.ValidKeys(ctx)
			if err != nil || len(got) != 2 || got[0] != second.Current || got[1] != first.Current {
				t.Fatalf("valid keys = %v, %v", got, err)
			}
		})
	}
}
