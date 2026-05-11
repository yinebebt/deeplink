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

// testBaseURL is the canonical base URL used across preview/handler tests.
const testBaseURL = "https://link.test/"

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

	store := NewMemoryStore()
	service, err := New(Config{
		BaseURL: testBaseURL,
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
	shortID := strings.TrimPrefix(shortURL, testBaseURL)
	if shortID == shortURL || shortID == "" {
		t.Fatalf("expected short URL with base %q, got %q", testBaseURL, shortURL)
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

	service, err := New(Config{
		BaseURL:     testBaseURL,
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

	shortID := strings.TrimPrefix(response["short_url"], testBaseURL)
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

// newPreviewService builds a Service against the real default templates with
// RedirectProcessor registered.
func newPreviewService(t *testing.T, cfg Config) *Service {
	t.Helper()
	if cfg.BaseURL == "" {
		cfg.BaseURL = testBaseURL
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	if cfg.TemplateDir == "" {
		cfg.TemplateDir = "templates/default"
	}
	service, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.Register(RedirectProcessor{})
	t.Cleanup(func() { _ = service.Close() })
	return service
}

// createAndFetch posts a Link and GETs the preview at getPathPrefix+shortID
// ("/" for handlePreview, "/preview/" for handleStaticPreview).
func createAndFetch(t *testing.T, service *Service, payloadJSON, getPathPrefix string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(payloadJSON))
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	shortID := strings.TrimPrefix(resp["short_url"], service.config.BaseURL)

	req = httptest.NewRequest(http.MethodGet, getPathPrefix+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d; body = %s", rec.Code, rec.Body.String())
	}
	return rec, shortID
}

func TestPreviewMetaTags_AllFields(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{
		SiteName:         "Example",
		Locale:           "fr_FR",
		TwitterSite:      "@example",
		FediverseCreator: "@example@instance.tld",
	})

	payload := `{
		"type": "redirect",
		"url": "https://example.com/post",
		"title": "Hello",
		"description": "World",
		"image_url": "https://cdn.example.com/img.png",
		"image_width": 1200,
		"image_height": 630,
		"image_alt": "Cover image",
		"og_type": "website",
		"locale": "en_GB"
	}`

	rec, shortID := createAndFetch(t, service, payload, "/")
	body := rec.Body.String()

	want := []string{
		`<link rel="canonical" href="https://link.test/` + shortID + `">`,
		`<meta name="robots" content="noindex,follow">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:url" content="https://link.test/` + shortID + `">`,
		`<meta property="og:title" content="Hello">`,
		`<meta property="og:description" content="World">`,
		`<meta property="og:site_name" content="Example">`,
		`<meta property="og:locale" content="en_GB">`,
		`<meta property="og:image" content="https://cdn.example.com/img.png">`,
		`<meta property="og:image:width" content="1200">`,
		`<meta property="og:image:height" content="630">`,
		`<meta property="og:image:alt" content="Cover image">`,
		`<meta property="og:updated_time" content="`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="twitter:title" content="Hello">`,
		`<meta name="twitter:description" content="World">`,
		`<meta name="twitter:image" content="https://cdn.example.com/img.png">`,
		`<meta name="twitter:image:alt" content="Cover image">`,
		`<meta name="twitter:site" content="@example">`,
		`<meta name="fediverse:creator" content="@example@instance.tld">`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing tag: %s\nbody:\n%s", w, body)
		}
	}
}

func TestPreviewMetaTags_OmitEmpty(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{Locale: ""})

	payload := `{"type":"redirect","url":"https://example.com/min"}`

	rec, _ := createAndFetch(t, service, payload, "/")
	body := rec.Body.String()

	if strings.Contains(body, `content=""`) {
		t.Errorf("found empty content attribute in body:\n%s", body)
	}

	forbidden := []string{
		"og:image",
		"og:image:width",
		"og:image:height",
		"og:image:alt",
		"twitter:image",
		"twitter:image:alt",
		"twitter:site",
		"fediverse:creator",
		"og:site_name",
		"article:published_time",
		"article:modified_time",
	}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("unexpected tag/marker %q in body:\n%s", f, body)
		}
	}

	if !strings.Contains(body, `<meta name="twitter:card" content="summary">`) {
		t.Errorf("expected twitter:card=summary fallback")
	}
}

func TestPreviewMetaTags_TwitterCardFallback(t *testing.T) {
	t.Parallel()

	t.Run("no image -> summary", func(t *testing.T) {
		t.Parallel()
		service := newPreviewService(t, Config{})
		rec, _ := createAndFetch(t, service, `{"type":"redirect","url":"https://example.com/a"}`, "/")
		if !strings.Contains(rec.Body.String(), `<meta name="twitter:card" content="summary">`) {
			t.Errorf("want summary card, body:\n%s", rec.Body.String())
		}
	})

	t.Run("image -> summary_large_image", func(t *testing.T) {
		t.Parallel()
		service := newPreviewService(t, Config{})
		payload := `{"type":"redirect","url":"https://example.com/b","image_url":"https://cdn.example.com/x.png"}`
		rec, _ := createAndFetch(t, service, payload, "/")
		if !strings.Contains(rec.Body.String(), `<meta name="twitter:card" content="summary_large_image">`) {
			t.Errorf("want summary_large_image, body:\n%s", rec.Body.String())
		}
	})
}

func TestPreviewMetaTags_Canonical(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{BaseURL: "https://canonical.test/"})

	rec, shortID := createAndFetch(t, service, `{"type":"redirect","url":"https://example.com/c"}`, "/")
	want := `<link rel="canonical" href="https://canonical.test/` + shortID + `">`
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("missing canonical link: %s\nbody:\n%s", want, rec.Body.String())
	}
}

func TestPreviewMetaTags_RobotsNoindex(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{})
	rec, _ := createAndFetch(t, service, `{"type":"redirect","url":"https://example.com/d"}`, "/")
	if !strings.Contains(rec.Body.String(), `<meta name="robots" content="noindex,follow">`) {
		t.Errorf("missing robots meta tag, body:\n%s", rec.Body.String())
	}
}

func TestStaticPreviewCacheControl(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{
		WebFallbackURL: "https://example.com/fallback",
	})

	livePreview, shortID := createAndFetch(t, service, `{"type":"redirect","url":"https://example.com/e"}`, "/")
	if got := livePreview.Header().Get("Cache-Control"); got != previewCacheControl {
		t.Fatalf("live Cache-Control = %q, want %q", got, previewCacheControl)
	}

	req := httptest.NewRequest(http.MethodGet, "/preview/"+shortID, nil)
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("static preview status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != previewCacheControl {
		t.Errorf("static Cache-Control = %q, want %q", got, previewCacheControl)
	}
}

func TestPreviewMetaTags_ArticleType(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{
		WebFallbackURL: "https://example.com/fallback",
		SiteName:       "Example",
	})

	payload := `{
		"type": "redirect",
		"url": "https://example.com/article",
		"title": "Article title",
		"description": "Article body",
		"image_url": "https://cdn.example.com/cover.png",
		"og_type": "article"
	}`

	rec, _ := createAndFetch(t, service, payload, "/preview/")
	body := rec.Body.String()

	want := []string{
		`<meta property="og:type" content="article">`,
		`<meta property="article:published_time" content="`,
		`<meta property="article:modified_time" content="`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing article marker %q, body:\n%s", w, body)
		}
	}
}

// Mixed-case and padded OGType must still trigger article tags. The
// previewView wrapper normalizes the value so the template's exact-string
// comparison matches.
func TestPreviewMetaTags_OGTypeNormalized(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{
		WebFallbackURL: "https://example.com/fallback",
	})

	payload := `{
		"type": "redirect",
		"url": "https://example.com/normalized",
		"title": "Mixed case",
		"og_type": "  Article  "
	}`

	rec, _ := createAndFetch(t, service, payload, "/preview/")
	body := rec.Body.String()

	for _, w := range []string{
		`<meta property="og:type" content="article">`,
		`<meta property="article:published_time" content="`,
	} {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q after OGType normalization, body:\n%s", w, body)
		}
	}
}

// Locale resolution chain: Link.Locale > Config.Locale > defaultLocale.
// Lang attribute on <html> is derived from the resolved locale.
func TestPreviewMetaTags_LocaleFallback(t *testing.T) {
	t.Parallel()

	t.Run("link locale wins", func(t *testing.T) {
		t.Parallel()
		service := newPreviewService(t, Config{Locale: "fr_FR"})
		rec, _ := createAndFetch(t, service,
			`{"type":"redirect","url":"https://example.com/loc1","locale":"de_DE"}`, "/")
		body := rec.Body.String()
		if !strings.Contains(body, `<meta property="og:locale" content="de_DE">`) {
			t.Errorf("link locale should win, body:\n%s", body)
		}
		if !strings.Contains(body, `<html lang="de"`) {
			t.Errorf("expected lang=\"de\" derived from de_DE, body:\n%s", body)
		}
	})

	t.Run("config locale fills when link empty", func(t *testing.T) {
		t.Parallel()
		service := newPreviewService(t, Config{Locale: "fr_FR"})
		rec, _ := createAndFetch(t, service,
			`{"type":"redirect","url":"https://example.com/loc2"}`, "/")
		body := rec.Body.String()
		if !strings.Contains(body, `<meta property="og:locale" content="fr_FR">`) {
			t.Errorf("config locale should fill, body:\n%s", body)
		}
		if !strings.Contains(body, `<html lang="fr"`) {
			t.Errorf("expected lang=\"fr\" derived from fr_FR, body:\n%s", body)
		}
	})

	t.Run("default locale when both empty", func(t *testing.T) {
		t.Parallel()
		// Config.Locale empty -> defaults() sets defaultLocale.
		service := newPreviewService(t, Config{})
		rec, _ := createAndFetch(t, service,
			`{"type":"redirect","url":"https://example.com/loc3"}`, "/")
		body := rec.Body.String()
		want := `<meta property="og:locale" content="` + defaultLocale + `">`
		if !strings.Contains(body, want) {
			t.Errorf("expected default locale tag, body:\n%s", body)
		}
		if !strings.Contains(body, `<html lang="en"`) {
			t.Errorf("expected lang=\"en\" derived from default locale, body:\n%s", body)
		}
	})
}

// Empty Title must still produce a valid <title> element so the page is
// HTML5-conformant. We fall back to "Untitled" rather than emitting an
// empty element.
func TestPreviewMetaTags_TitleFallback(t *testing.T) {
	t.Parallel()

	service, err := New(Config{
		BaseURL:     testBaseURL,
		Store:       NewMemoryStore(),
		TemplateDir: "templates/default",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.Register(noTitleProcessor{})
	t.Cleanup(func() { _ = service.Close() })

	req := httptest.NewRequest(http.MethodPost, "/shorten",
		strings.NewReader(`{"type":"no-title","url":"https://example.com/no-title"}`))
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	shortID := strings.TrimPrefix(resp["short_url"], testBaseURL)

	req = httptest.NewRequest(http.MethodGet, "/"+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if strings.Contains(body, "<title></title>") {
		t.Errorf("empty <title> emitted, body:\n%s", body)
	}
	if !strings.Contains(body, "<title>Open link</title>") {
		t.Errorf("expected fallback title, body:\n%s", body)
	}
	// og:title is gated on Link.Title (no fallback) -> must be absent.
	if strings.Contains(body, "og:title") {
		t.Errorf("og:title should be omitted when Title empty, body:\n%s", body)
	}
}

// Non-article OGType values must emit og:type but skip article:* tags.
func TestPreviewMetaTags_NonArticleOGTypeSkipsArticleTags(t *testing.T) {
	t.Parallel()

	for _, ogType := range []string{"profile", "book", "video.movie"} {
		t.Run(ogType, func(t *testing.T) {
			t.Parallel()
			service := newPreviewService(t, Config{
				WebFallbackURL: "https://example.com/fallback",
			})
			payload := `{
				"type": "redirect",
				"url": "https://example.com/x",
				"title": "Sample",
				"og_type": "` + ogType + `"
			}`
			rec, _ := createAndFetch(t, service, payload, "/preview/")
			body := rec.Body.String()

			if !strings.Contains(body, `<meta property="og:type" content="`+ogType+`">`) {
				t.Errorf("missing og:type=%s, body:\n%s", ogType, body)
			}
			for _, forbidden := range []string{
				"article:published_time",
				"article:modified_time",
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("og:type=%s should not emit %q, body:\n%s", ogType, forbidden, body)
				}
			}
		})
	}
}

func TestLangFromLocale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"", "en"},
		{"en_US", "en"},
		{"fr-FR", "fr"},
		{"de_DE", "de"},
		{"PT_BR", "pt"},
		{"es", "es"},
		{"ES", "es"},
	}
	for _, c := range cases {
		if got := langFromLocale(c.in); got != c.want {
			t.Errorf("langFromLocale(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type noTitleProcessor struct{}

func (noTitleProcessor) Type() string { return "no-title" }
func (noTitleProcessor) Process(_ context.Context, payload *Link) error {
	if payload.URL == "" {
		return NewError(errors.New("missing url"), http.StatusBadRequest, "url is required")
	}
	return nil
}

// createLinkForTest posts to /shorten and returns the new short ID.
func createLinkForTest(t *testing.T, service *Service, payloadJSON string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(payloadJSON))
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return strings.TrimPrefix(resp["short_url"], service.config.BaseURL)
}

func TestHandleUpdate(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{})

	shortID := createLinkForTest(t, service,
		`{"type":"redirect","url":"https://example.com/before","title":"Before"}`)

	patch := `{"title":"After","image_url":"https://cdn.example.com/x.png","image_width":1200}`
	req := httptest.NewRequest(http.MethodPatch, "/"+shortID, strings.NewReader(patch))
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d; body = %s", rec.Code, rec.Body.String())
	}

	var got LinkResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if got.Title != "After" {
		t.Errorf("Title = %q, want %q", got.Title, "After")
	}
	if got.ImageURL != "https://cdn.example.com/x.png" {
		t.Errorf("ImageURL = %q", got.ImageURL)
	}
	if got.ImageWidth != 1200 {
		t.Errorf("ImageWidth = %d, want 1200", got.ImageWidth)
	}
	if got.URL != "https://example.com/before" {
		t.Errorf("URL unexpectedly changed to %q", got.URL)
	}
	if got.UpdatedAt == "" {
		t.Error("UpdatedAt not set")
	}
}

func TestHandleUpdateNotFound(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{})
	req := httptest.NewRequest(http.MethodPatch, "/missing", strings.NewReader(`{"title":"x"}`))
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleDeleteSoftDeletes(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	service := newPreviewService(t, Config{Store: store})

	shortID := createLinkForTest(t, service,
		`{"type":"redirect","url":"https://example.com/del"}`)

	req := httptest.NewRequest(http.MethodDelete, "/"+shortID, nil)
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	// Preview should now 404.
	req = httptest.NewRequest(http.MethodGet, "/"+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("preview after delete status = %d, want 404", rec.Code)
	}

	// Underlying record is retained during the grace window with DeletedAt set.
	stored, err := store.Get(context.Background(), shortID)
	if err != nil {
		t.Fatalf("expected record to remain within grace window, got %v", err)
	}
	if stored.DeletedAt == "" {
		t.Error("DeletedAt not set on stored payload")
	}

	// Double-delete returns 404 (already soft-deleted).
	req = httptest.NewRequest(http.MethodDelete, "/"+shortID, nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rec.Code)
	}
}

func TestHandleLinkListAll(t *testing.T) {
	t.Parallel()

	service := newPreviewService(t, Config{})

	a := createLinkForTest(t, service, `{"type":"redirect","url":"https://example.com/a","title":"A"}`)
	b := createLinkForTest(t, service, `{"type":"redirect","url":"https://example.com/b","title":"B"}`)

	// Soft-delete one to verify it is filtered out.
	req := httptest.NewRequest(http.MethodDelete, "/"+b, nil)
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/links", nil)
	rec = httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/links status = %d", rec.Code)
	}

	var resp []LinkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("got %d entries, want 1 (b should be filtered)", len(resp))
	}
	if resp[0].ShortID != a {
		t.Errorf("ShortID = %q, want %q", resp[0].ShortID, a)
	}
	if resp[0].ShortLink != testBaseURL+a {
		t.Errorf("ShortLink = %q", resp[0].ShortLink)
	}
}
