package deeplink

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestClickTrackerFlushesOnStop(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	// Save a link so IncrClick has something to increment.
	_ = store.Save(context.Background(), "abc", &Link{Type: "basic", URL: "https://example.com"})

	ct := newClickTracker(store, slog.Default(), 64, 10*time.Second)

	ct.track("abc")
	ct.track("abc")
	ct.track("abc")

	// Stop drains the buffer.
	ct.stop()

	clicks, err := store.Clicks(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Clicks() error = %v", err)
	}
	if clicks != 3 {
		t.Fatalf("clicks = %d, want 3", clicks)
	}
}

func TestClickTrackerFlushesOnInterval(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	_ = store.Save(context.Background(), "xyz", &Link{Type: "basic", URL: "https://example.com"})

	ct := newClickTracker(store, slog.Default(), 64, 50*time.Millisecond)
	defer ct.stop()

	ct.track("xyz")
	ct.track("xyz")

	// Wait for at least one flush cycle.
	time.Sleep(150 * time.Millisecond)

	clicks, err := store.Clicks(context.Background(), "xyz")
	if err != nil {
		t.Fatalf("Clicks() error = %v", err)
	}
	if clicks != 2 {
		t.Fatalf("clicks = %d, want 2", clicks)
	}
}

func TestClickTrackerDropsWhenBufferFull(t *testing.T) {
	t.Parallel()

	// Test track() in isolation — no consumer goroutine, so the buffer
	// fills deterministically and the third send hits the drop path.
	ct := &clickTracker{
		logger: slog.Default(),
		events: make(chan string, 2),
	}

	ct.track("full")
	ct.track("full")
	ct.track("full") // dropped

	if got := len(ct.events); got != 2 {
		t.Fatalf("buffered events = %d, want 2 (third should be dropped)", got)
	}
}
