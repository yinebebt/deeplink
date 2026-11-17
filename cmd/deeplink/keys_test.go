package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/yinebebt/keyring"
)

// The server and `deeplink keys` share only the database: a rotation takes
// effect on the next request, the old key works through grace, revoke ends it.
func TestRotationWithoutRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deeplink.db")
	server := sqliteKeys(t, path)
	if err := seedAPIKey(ctx, server, "old", discard); err != nil {
		t.Fatal(err)
	}
	h := withAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), server, discard)
	check := func(key string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/shorten", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("key %q: status %d, want %d", key, rec.Code, want)
		}
	}

	kr := keyring.New(sqliteKeys(t, path), time.Hour)
	check("old", http.StatusNoContent)
	rotated, err := kr.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check(rotated.Current, http.StatusNoContent)
	check("old", http.StatusNoContent)
	if _, err := kr.Revoke(ctx); err != nil {
		t.Fatal(err)
	}
	check("old", http.StatusUnauthorized)
	check(rotated.Current, http.StatusNoContent)
}

func TestSeedAPIKey(t *testing.T) {
	ctx := context.Background()
	// A stale DEEPLINK_API_KEY must not undo a rotation on restart.
	store := storeWith(t, keyring.KeySet{Current: "rotated"})
	if err := seedAPIKey(ctx, store, "stale", discard); err != nil {
		t.Fatal(err)
	}
	if ks, _ := store.Get(ctx); ks.Current != "rotated" {
		t.Fatalf("store overwritten: %q", ks.Current)
	}
}
