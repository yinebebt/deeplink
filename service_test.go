package deeplink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testProcessor struct{}

func (testProcessor) Type() string { return "basic" }

func (testProcessor) Process(_ context.Context, payload *Link) error {
	rawURL := payload.Meta("url")
	if rawURL == "" {
		return NewError(errors.New("missing metadata.url"), http.StatusBadRequest, "metadata.url is required")
	}

	payload.URL = rawURL
	if t := payload.Meta("title"); t != "" {
		payload.Title = t
	}
	if d := payload.Meta("description"); d != "" {
		payload.Description = d
	}
	if img := payload.Meta("image_url"); img != "" {
		payload.ImageURL = img
	}

	if payload.Title == "" {
		payload.Title = "Test title"
	}
	if payload.Description == "" {
		payload.Description = "Test description"
	}

	return nil
}

func TestHandlerGenerateAndPreviewRedirect(t *testing.T) {
	t.Parallel()

	baseURL := "https://link.test/"
	store := NewMemoryStore()
	service, err := New(Config{
		BaseURL: baseURL,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.Register(testProcessor{})

	body := bytes.NewBufferString(`{
		"type": "basic",
		"metadata": {
			"url": "https://example.com/products/123",
			"title": "Example title",
			"description": "Example description"
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/shorten", body)
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}

	shortURL := response["short_url"]
	shortID := strings.TrimPrefix(shortURL, baseURL)
	if shortID == shortURL || shortID == "" {
		t.Fatalf("expected short URL with base %q, got %q", baseURL, shortURL)
	}

	stored, err := store.Get(context.Background(), shortID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if stored.ShortID != shortID {
		t.Fatalf("stored ShortID = %q, want %q", stored.ShortID, shortID)
	}
	if stored.URL != "https://example.com/products/123" {
		t.Fatalf("stored URL = %q, want destination URL", stored.URL)
	}

	// GET /{shortID} without templates should redirect
	req = httptest.NewRequest(http.MethodGet, "/"+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("preview status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "https://example.com/products/123" {
		t.Fatalf("redirect location = %q, want %q", location, "https://example.com/products/123")
	}

	// Flush async click buffer before checking count.
	if err := service.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	clicks, err := store.Clicks(context.Background(), shortID)
	if err != nil {
		t.Fatalf("Clicks() error = %v", err)
	}
	if clicks != 1 {
		t.Fatalf("clicks = %d, want 1", clicks)
	}
}

func TestNewLoadsTemplates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "link.html"), []byte(`<html><body>{{.Title}}</body></html>`), 0o600); err != nil {
		t.Fatalf("write link template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preview.html"), []byte(`<html><body>{{.Description}}</body></html>`), 0o600); err != nil {
		t.Fatalf("write preview template: %v", err)
	}

	baseURL := "https://link.test/"
	service, err := New(Config{
		BaseURL:     baseURL,
		Store:       NewMemoryStore(),
		TemplateDir: dir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.Register(testProcessor{})

	body := bytes.NewBufferString(`{
		"type": "basic",
		"metadata": {
			"url": "https://example.com/templated",
			"title": "Templated title"
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/shorten", body)
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}

	shortID := strings.TrimPrefix(response["short_url"], baseURL)
	req = httptest.NewRequest(http.MethodGet, "/"+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Templated title") {
		t.Fatalf("preview body = %q, want rendered title", got)
	}
}
