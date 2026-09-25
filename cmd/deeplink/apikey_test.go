package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestWithAPIKey_ProtectsList(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := withAPIKey(inner, "secret")

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
