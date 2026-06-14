package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yinebebt/deeplink"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dsn)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLite_SaveGetDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	link := &deeplink.Link{Type: "redirect", URL: "https://example.com", Title: "Hi", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := s.Save(ctx, "abc", link); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Get(ctx, "abc")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.URL != "https://example.com" || got.ShortID != "abc" {
		t.Fatalf("Get() = %+v", got)
	}

	if err := s.Delete(ctx, "abc"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	// Within grace the link is still returned, but flagged deleted.
	got, err = s.Get(ctx, "abc")
	if err != nil {
		t.Fatalf("Get(after delete) error = %v", err)
	}
	if got.DeletedAt == "" {
		t.Fatalf("expected DeletedAt set after soft-delete")
	}
	// Double delete is a no-op error.
	if err := s.Delete(ctx, "abc"); !errors.Is(err, deeplink.ErrNotFound) {
		t.Fatalf("Delete(twice) error = %v, want ErrNotFound", err)
	}
	// Re-save resurrects and clears DeletedAt.
	if err := s.Save(ctx, "abc", &deeplink.Link{Type: "redirect", URL: "https://example.com", CreatedAt: link.CreatedAt}); err != nil {
		t.Fatalf("Save(resurrect) error = %v", err)
	}
	got, _ = s.Get(ctx, "abc")
	if got.DeletedAt != "" {
		t.Fatalf("re-save should clear DeletedAt, got %q", got.DeletedAt)
	}
}

func TestSQLite_Expiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := s.Save(ctx, "old", &deeplink.Link{Type: "redirect", URL: "https://e.com", ExpiresAt: past}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := s.Get(ctx, "old"); !errors.Is(err, deeplink.ErrNotFound) {
		t.Fatalf("Get(expired) error = %v, want ErrNotFound", err)
	}
}

func TestSQLite_Clicks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Save(ctx, "c1", &deeplink.Link{Type: "redirect", URL: "https://e.com"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for i := int64(1); i <= 3; i++ {
		n, err := s.IncrClickBy(ctx, "c1", 1)
		if err != nil {
			t.Fatalf("IncrClickBy() error = %v", err)
		}
		if n != i {
			t.Fatalf("IncrClickBy() = %d, want %d", n, i)
		}
	}
	// A batched bump (delta > 1) is the common flusher path.
	if n, err := s.IncrClickBy(ctx, "c1", 5); err != nil || n != 8 {
		t.Fatalf("IncrClickBy(+5) = %d, %v; want 8", n, err)
	}
	n, _ := s.Clicks(ctx, "c1")
	if n != 8 {
		t.Fatalf("Clicks() = %d, want 8", n)
	}
	// Missing link increments to nothing, no error.
	if n, err := s.IncrClickBy(ctx, "missing", 1); err != nil || n != 0 {
		t.Fatalf("IncrClickBy(missing) = %d, %v", n, err)
	}
}

func TestSQLite_ListPagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Now().UTC()
	for i := range 5 {
		id := string(rune('a' + i))
		// Distinct created_at keeps ordering deterministic.
		created := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if err := s.Save(ctx, id, &deeplink.Link{Type: "redirect", URL: "https://e.com", CreatedAt: created}); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	var seen int
	var cursor uint64
	for {
		links, next, err := s.List(ctx, "redirect", cursor, 2)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		seen += len(links)
		if next == 0 {
			break
		}
		cursor = next
	}
	if seen != 5 {
		t.Fatalf("paginated total = %d, want 5", seen)
	}
}

// TestSQLite_ListPaginationExactMultiple covers the boundary where the row
// count is an exact multiple of the page size: the last full page returns a
// non-zero cursor, so a final query yields an empty page before next == 0.
func TestSQLite_ListPaginationExactMultiple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Now().UTC()
	for i := range 4 {
		id := string(rune('a' + i))
		created := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if err := s.Save(ctx, id, &deeplink.Link{Type: "redirect", URL: "https://e.com", CreatedAt: created}); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	var pages, nonEmpty, seen int
	var cursor uint64
	for {
		links, next, err := s.List(ctx, "redirect", cursor, 2)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		pages++
		if len(links) > 0 {
			nonEmpty++
		}
		seen += len(links)
		if next == 0 {
			break
		}
		cursor = next
	}
	if seen != 4 {
		t.Fatalf("total = %d, want 4", seen)
	}
	if nonEmpty != 2 {
		t.Fatalf("non-empty pages = %d, want 2", nonEmpty)
	}
	if pages != 3 {
		t.Fatalf("total pages = %d, want 3 (2 full + 1 trailing empty)", pages)
	}
}

func TestSQLite_Analytics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Save(ctx, "a", &deeplink.Link{Type: "redirect", URL: "https://e.com"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	day := time.Date(2026, 6, 14, 9, 0, 0, 0, time.UTC)
	events := []deeplink.ClickEvent{
		{ShortID: "a", Time: day, Platform: "android", Referrer: "t.co"},
		{ShortID: "a", Time: day, Platform: "android"},
		{ShortID: "a", Time: day, Platform: "web", Referrer: "t.co"},
	}
	if err := s.RecordEvents(ctx, events); err != nil {
		t.Fatalf("RecordEvents() error = %v", err)
	}

	stats, err := s.Stats(ctx, "a")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.ByPlatform["android"] != 2 || stats.ByPlatform["web"] != 1 {
		t.Fatalf("by_platform = %v", stats.ByPlatform)
	}
	if stats.ByReferrer["t.co"] != 2 {
		t.Fatalf("by_referrer = %v", stats.ByReferrer)
	}
	if stats.ByDay["2026-06-14"] != 3 {
		t.Fatalf("by_day = %v", stats.ByDay)
	}
}
