package deeplink

import (
	"context"
	"time"
)

// DeleteGracePeriod is the window during which soft-deleted links remain
// in the backing store before being purged. Stores back this with a TTL
// (Redis EXPIRE) or equivalent lazy cleanup.
const DeleteGracePeriod = 3 * time.Hour

// Store defines the persistence interface for links and click counts.
type Store interface {
	// Save stores a payload under the given short ID.
	Save(ctx context.Context, id string, payload *Link) error
	// Get retrieves a payload by short ID. Returns [ErrNotFound] if missing.
	// Soft-deleted payloads (within the grace window) are returned with
	// [Link.DeletedAt] set; callers decide whether to expose them.
	Get(ctx context.Context, id string) (*Link, error)
	// Delete soft-deletes a payload: marks [Link.DeletedAt] and schedules
	// purge after [DeleteGracePeriod]. Returns [ErrNotFound] if the id
	// does not exist or is already past grace.
	Delete(ctx context.Context, id string) error
	// IncrClick increments the click counter for id and returns the new count.
	IncrClick(ctx context.Context, id string) (int64, error)
	// Clicks returns the current click count for id.
	Clicks(ctx context.Context, id string) (int64, error)
	// List returns links matching linkType.
	// Pass cursor 0 to start. The returned cursor is 0 when there are
	// no more results. Cursor values are opaque and store-specific;
	// do not assume sequential offsets. Soft-deleted links are omitted.
	// Returned [LinkResponse.ShortLink] values contain only the short ID;
	// the HTTP layer prefixes them with the configured base URL.
	List(ctx context.Context, linkType string, cursor uint64, count int64) ([]LinkResponse, uint64, error)
}
