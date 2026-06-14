package deeplink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemoryStore_ExpiryEviction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryStore()

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := store.Save(ctx, "gone", &Link{Type: "redirect", URL: "https://e.com", ExpiresAt: past}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := store.Get(ctx, "gone"); err != ErrNotFound {
		t.Fatalf("Get(expired) error = %v, want ErrNotFound", err)
	}

	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := store.Save(ctx, "live", &Link{Type: "redirect", URL: "https://e.com", ExpiresAt: future}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := store.Get(ctx, "live"); err != nil {
		t.Fatalf("Get(live) error = %v, want nil", err)
	}

	// Expired links must not surface in List either.
	links, _, err := store.List(ctx, "redirect", 0, 100)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(links) != 1 || links[0].ShortLink != "live" {
		t.Fatalf("List() = %+v, want only [live]", links)
	}
}

func TestShorten_RejectsBadExpiry(t *testing.T) {
	t.Parallel()
	svc := newPreviewService(t, Config{})

	cases := map[string]string{
		"unparseable": `{"type":"redirect","url":"https://e.com","expires_at":"not-a-date"}`,
		"in the past": fmt.Sprintf(`{"type":"redirect","url":"https://e.com","expires_at":%q}`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
			rec := httptest.NewRecorder()
			svc.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLinkTemplate_PlatformFallback(t *testing.T) {
	t.Parallel()
	svc := newPreviewService(t, Config{
		AndroidStoreURL: "https://play.google.com/store/apps/details?id=com.example",
		IOSStoreURL:     "https://apps.apple.com/app/id123",
		WebFallbackURL:  "https://example.com/get",
	})

	rec, _ := createAndFetch(t, svc, `{"type":"redirect","url":"https://example.com/deep"}`, "/")
	body := rec.Body.String()

	// html/template JS-escapes "/" as "\/" in the script context, so assert
	// on the (slash-free) hosts rather than full URLs.
	for _, want := range []string{"setTimeout", "play.google.com", "apps.apple.com"} {
		if !strings.Contains(body, want) {
			t.Fatalf("template body missing %q\n%s", want, body)
		}
	}
}

func TestLinkTemplate_NoStoreURLs_PlainForward(t *testing.T) {
	t.Parallel()
	svc := newPreviewService(t, Config{}) // no store URLs

	rec, _ := createAndFetch(t, svc, `{"type":"redirect","url":"https://example.com/deep"}`, "/")
	body := rec.Body.String()

	// The setTimeout is static script text gated at runtime by (fallback !==
	// dest); with no store URLs the template vars render empty, so the
	// fallback equals the destination and the timer never arms.
	for _, want := range []string{`const android = "";`, `const ios = "";`, `const web = "";`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected empty store var %q\n%s", want, body)
		}
	}
	if !strings.Contains(body, "example.com") {
		t.Fatalf("expected destination host in body\n%s", body)
	}
}

func TestNewClickEvent(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120 Mobile")
	req.Header.Set("Referer", "https://t.co/xyz")

	ev := newClickEvent(req, "abc")
	if ev.ShortID != "abc" {
		t.Fatalf("ShortID = %q", ev.ShortID)
	}
	if ev.Platform != "android" || ev.Device != "mobile" {
		t.Fatalf("platform/device = %q/%q, want android/mobile", ev.Platform, ev.Device)
	}
	if ev.Browser != "Chrome" {
		t.Fatalf("browser = %q, want Chrome", ev.Browser)
	}
	if ev.Referrer != "t.co" {
		t.Fatalf("referrer = %q, want t.co", ev.Referrer)
	}
}

func TestMemoryStore_Analytics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryStore()

	day := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	events := []ClickEvent{
		{ShortID: "a", Time: day, Platform: "android", Referrer: "t.co"},
		{ShortID: "a", Time: day, Platform: "ios", Referrer: ""},
		{ShortID: "a", Time: day, Platform: "android", Referrer: "t.co"},
	}
	if err := store.RecordEvents(ctx, events); err != nil {
		t.Fatalf("RecordEvents() error = %v", err)
	}

	stats, err := store.Stats(ctx, "a")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.ByPlatform["android"] != 2 || stats.ByPlatform["ios"] != 1 {
		t.Fatalf("by_platform = %v", stats.ByPlatform)
	}
	// Empty referrer must not become a "" bucket.
	if _, ok := stats.ByReferrer[""]; ok {
		t.Fatalf("by_referrer has empty key: %v", stats.ByReferrer)
	}
	if stats.ByDay["2026-06-14"] != 3 {
		t.Fatalf("by_day = %v", stats.ByDay)
	}
}

func TestStatsEndpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.Save(ctx, "abc", &Link{Type: "redirect", URL: "https://e.com"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.RecordEvents(ctx, []ClickEvent{{ShortID: "abc", Time: time.Now().UTC(), Platform: "web"}}); err != nil {
		t.Fatalf("RecordEvents() error = %v", err)
	}

	svc := newPreviewService(t, Config{Store: store})

	req := httptest.NewRequest(http.MethodGet, "/stats/abc", nil)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}

	var stats Stats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.ByPlatform["web"] != 1 {
		t.Fatalf("by_platform = %v", stats.ByPlatform)
	}
}

// TestVisitPipeline_EndToEnd exercises the whole async path: a real resolve
// request → UA parse → click event → background flush → counter + analytics
// → /stats, which no other test covers together.
func TestVisitPipeline_EndToEnd(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	svc := newPreviewService(t, Config{Store: store, ClickFlushInterval: 20 * time.Millisecond})

	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(`{"type":"redirect","url":"https://example.com/x"}`))
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	id := strings.TrimPrefix(resp["short_url"], svc.config.BaseURL)

	// Two visits from an Android browser referred by t.co.
	for range 2 {
		v := httptest.NewRequest(http.MethodGet, "/"+id, nil)
		v.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14) Chrome/120 Mobile")
		v.Header.Set("Referer", "https://t.co/abc")
		svc.Handler().ServeHTTP(httptest.NewRecorder(), v)
	}

	// Wait for at least one flush cycle.
	deadline := time.Now().Add(2 * time.Second)
	var stats Stats
	for time.Now().Before(deadline) {
		sr := httptest.NewRequest(http.MethodGet, "/stats/"+id, nil)
		srec := httptest.NewRecorder()
		svc.Handler().ServeHTTP(srec, sr)
		_ = json.NewDecoder(srec.Body).Decode(&stats)
		if stats.Clicks >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if stats.Clicks != 2 {
		t.Fatalf("clicks = %d, want 2", stats.Clicks)
	}
	if stats.ByPlatform["android"] != 2 {
		t.Fatalf("by_platform = %v, want android:2", stats.ByPlatform)
	}
	if stats.ByReferrer["t.co"] != 2 {
		t.Fatalf("by_referrer = %v, want t.co:2", stats.ByReferrer)
	}
}

func TestParseUserAgent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                          string
		ua                            string
		platform, device, os, browser string
	}{
		{"empty", "", platformWeb, deviceDesktop, "", ""},
		{"android chrome", "Mozilla/5.0 (Linux; Android 14) Chrome/120 Mobile", platformAndroid, deviceMobile, "Android", "Chrome"},
		{"iphone safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) Version/17.0 Mobile Safari", platformIOS, deviceMobile, "iOS", "Safari"},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 17_0) Mobile", platformIOS, deviceMobile, "iPadOS", ""},
		{"twitterbot", "Twitterbot/1.0", platformWeb, deviceBot, "", ""},
		{"facebook crawler", "facebookexternalhit/1.1", platformWeb, deviceBot, "", ""},
		{"edge before chrome", "Mozilla/5.0 (Windows NT 10.0) Chrome/120 Edg/120", platformWeb, deviceDesktop, "Windows", "Edge"},
		{"opera", "Mozilla/5.0 (Windows NT 10.0) Chrome/120 OPR/106", platformWeb, deviceDesktop, "Windows", "Opera"},
		{"firefox mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15) Firefox/121", platformWeb, deviceDesktop, "macOS", "Firefox"},
		{"chrome on ios (crios)", "Mozilla/5.0 (iPhone) CriOS/120 Mobile", platformIOS, deviceMobile, "iOS", "Chrome"},
		{"firefox on ios (fxios)", "Mozilla/5.0 (iPhone) FxiOS/121 Mobile Safari", platformIOS, deviceMobile, "iOS", "Firefox"},
		{"edge on android (edga)", "Mozilla/5.0 (Linux; Android 14) Chrome/120 Mobile EdgA/120", platformAndroid, deviceMobile, "Android", "Edge"},
		{"linux desktop", "Mozilla/5.0 (X11; Linux x86_64) Chrome/120", platformWeb, deviceDesktop, "Linux", "Chrome"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, d, o, b := parseUserAgent(tc.ua)
			if p != tc.platform || d != tc.device || o != tc.os || b != tc.browser {
				t.Fatalf("parseUserAgent(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					tc.ua, p, d, o, b, tc.platform, tc.device, tc.os, tc.browser)
			}
		})
	}
}

func TestRedirectProcessor_RejectsNonHTTPScheme(t *testing.T) {
	t.Parallel()
	p := RedirectProcessor{}
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,<script>1</script>", "vbscript:msgbox(1)"} {
		link := &Link{URL: bad}
		if err := p.Process(context.Background(), link); err == nil {
			t.Fatalf("Process(%q) = nil, want rejection", bad)
		}
	}
	for _, ok := range []string{"https://example.com/x", "http://example.com"} {
		if err := p.Process(context.Background(), &Link{URL: ok}); err != nil {
			t.Fatalf("Process(%q) = %v, want nil", ok, err)
		}
	}
}
