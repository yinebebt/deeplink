package deeplink

import "context"

// Store defines the persistence interface for links and click counts.
type Store interface {
	// Save stores a payload under the given short ID.
	Save(ctx context.Context, id string, payload *Link) error
	// Get retrieves a payload by short ID. Returns [ErrNotFound] if missing.
	Get(ctx context.Context, id string) (*Link, error)
	// IncrClick increments the click counter for id and returns the new count.
	IncrClick(ctx context.Context, id string) (int64, error)
	// Clicks returns the current click count for id.
	Clicks(ctx context.Context, id string) (int64, error)
	// List returns links matching linkType.
	// Pass cursor 0 to start. The returned cursor is 0 when there are
	// no more results. Cursor values are opaque and store-specific;
	// do not assume sequential offsets.
	// Returned [LinkInfo.ShortLink] values contain only the short ID (no base URL).
	List(ctx context.Context, linkType string, cursor uint64, count int64) ([]LinkInfo, uint64, error)
}
