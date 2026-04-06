package deeplink

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// clickTracker buffers click events and flushes them to the store
// in the background, keeping the redirect path fast.
type clickTracker struct {
	store    Store
	logger   *slog.Logger
	events   chan string
	interval time.Duration
	done     chan struct{}
	wg       sync.WaitGroup
}

func newClickTracker(store Store, logger *slog.Logger, bufSize int, interval time.Duration) *clickTracker {
	ct := &clickTracker{
		store:    store,
		logger:   logger,
		events:   make(chan string, bufSize),
		interval: interval,
		done:     make(chan struct{}),
	}
	ct.wg.Add(1)
	go ct.run()
	return ct
}

// track enqueues a click event. If the buffer is full the event is dropped
// and a warning is logged — the redirect is never blocked.
func (ct *clickTracker) track(shortID string) {
	select {
	case ct.events <- shortID:
	default:
		ct.logger.Warn("click buffer full, dropping event", "shortID", shortID)
	}
}

// stop signals the background worker to drain remaining events and exit.
func (ct *clickTracker) stop() {
	close(ct.done)
	ct.wg.Wait()
}

// run is the background loop. It collects IDs into a batch and flushes
// either when the batch interval fires or when stop is called.
func (ct *clickTracker) run() {
	defer ct.wg.Done()

	ticker := time.NewTicker(ct.interval)
	defer ticker.Stop()

	batch := make(map[string]int64)

	for {
		select {
		case id := <-ct.events:
			batch[id]++

		case <-ticker.C:
			ct.flush(batch)
			batch = make(map[string]int64)

		case <-ct.done:
			// Drain remaining events from the channel.
			for {
				select {
				case id := <-ct.events:
					batch[id]++
				default:
					ct.flush(batch)
					return
				}
			}
		}
	}
}

// flush writes accumulated counts to the store.
func (ct *clickTracker) flush(batch map[string]int64) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for id, count := range batch {
		for range count {
			if _, err := ct.store.IncrClick(ctx, id); err != nil {
				ct.logger.Warn("failed to flush click", "error", err, "shortID", id)
			}
		}
	}
}
