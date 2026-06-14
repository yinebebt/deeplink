package deeplink

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// clickTracker buffers click events and flushes them to the store in the
// background, keeping the redirect path fast. Each flush bumps the per-link
// click counter and records the raw events for analytics breakdowns.
type clickTracker struct {
	store    Store
	logger   *slog.Logger
	events   chan ClickEvent
	interval time.Duration
	done     chan struct{}
	wg       sync.WaitGroup
}

func newClickTracker(store Store, logger *slog.Logger, bufSize int, interval time.Duration) *clickTracker {
	ct := &clickTracker{
		store:    store,
		logger:   logger,
		events:   make(chan ClickEvent, bufSize),
		interval: interval,
		done:     make(chan struct{}),
	}
	ct.wg.Add(1)
	go ct.run()
	return ct
}

// track enqueues a click event. If the buffer is full the event is dropped
// and a warning is logged — the redirect is never blocked.
func (ct *clickTracker) track(ev ClickEvent) {
	select {
	case ct.events <- ev:
	default:
		ct.logger.Warn("click buffer full, dropping event", "shortID", ev.ShortID)
	}
}

// stop signals the background worker to drain remaining events and exit.
func (ct *clickTracker) stop() {
	close(ct.done)
	ct.wg.Wait()
}

// run is the background loop. It collects events into a batch and flushes
// either when the batch interval fires or when stop is called.
func (ct *clickTracker) run() {
	defer ct.wg.Done()

	ticker := time.NewTicker(ct.interval)
	defer ticker.Stop()

	batch := make([]ClickEvent, 0, 64)

	for {
		select {
		case ev := <-ct.events:
			batch = append(batch, ev)

		case <-ticker.C:
			ct.flush(batch)
			batch = batch[:0]

		case <-ct.done:
			// Drain remaining events from the channel.
			for {
				select {
				case ev := <-ct.events:
					batch = append(batch, ev)
				default:
					ct.flush(batch)
					return
				}
			}
		}
	}
}

// flush bumps the counter once per link (aggregating the batch), then records
// the events for breakdowns. The two writes are independent, so breakdown
// sums may drift from the click total — treat breakdowns as approximate.
func (ct *clickTracker) flush(batch []ClickEvent) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	counts := make(map[string]int64, len(batch))
	for _, ev := range batch {
		counts[ev.ShortID]++
	}
	for id, count := range counts {
		if _, err := ct.store.IncrClickBy(ctx, id, count); err != nil {
			ct.logger.Warn("failed to flush clicks", "error", err, "shortID", id, "count", count)
		}
	}

	if err := ct.store.RecordEvents(ctx, batch); err != nil {
		ct.logger.Warn("failed to record click events", "error", err, "count", len(batch))
	}
}
