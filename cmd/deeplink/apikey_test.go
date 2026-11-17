package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yinebebt/keyring"
)

var discard = slog.New(slog.DiscardHandler)

// storeWith returns a key store holding ks.
func storeWith(t *testing.T, ks keyring.KeySet) keyring.Store {
	t.Helper()
	store := memKeyStore()
	if err := store.Put(context.Background(), ks); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRequiresAPIKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/shorten", true},
		{http.MethodPatch, "/abc", true},
		{http.MethodDelete, "/abc", true},
		{http.MethodGet, "/links", true},
		{http.MethodGet, "/links/", true},
		{http.MethodGet, "/links/redirect", true},
		{http.MethodGet, "/stats/abc", true},
		{http.MethodGet, "/mcp", true},
		{http.MethodPost, "/mcp", true},
		{http.MethodGet, "/abc", false},
		{http.MethodGet, "/health", false},
		{http.MethodGet, "/redirect", false},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			got := requiresAPIKey(tc.method, tc.path)
			if got != tc.want {
				t.Fatalf("requiresAPIKey(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

// With no key anywhere, protected routes are refused rather than open.
func TestWithAPIKey_EmptyStore(t *testing.T) {
	t.Parallel()

	h := withAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), memKeyStore(), discard)

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/shorten", http.StatusUnauthorized},
		{http.MethodGet, "/abc123", http.StatusNoContent},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

func TestWithAPIKey_ProtectsList(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := withAPIKey(inner, storeWith(t, keyring.KeySet{Current: "secret"}), discard)

	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/links", nil)
	req.Header.Set("X-API-Key", "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("with key: status = %d, want 204", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/abc123", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preview GET: status = %d, want 204", rec.Code)
	}
}
