// Package sqlitestore provides a persistent [deeplink.Store] backed by SQLite
// (modernc.org/sqlite), including the click-analytics breakdowns. The driver
// is only pulled in when this package is imported.
//
// Retention: click_events is append-only, pruned only when its link expires
// or is purged. For heavy workloads, add an age-based prune (by the day
// column) or roll up into daily aggregates.
package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yinebebt/deeplink"

	_ "modernc.org/sqlite"
)

var _ deeplink.Store = (*Store)(nil)

// Store is a SQLite-backed link store with analytics.
type Store struct {
	db *sql.DB
}

// New opens (creating if needed) a SQLite database at dsn and returns a ready
// Store. dsn is a file path, or "file::memory:?cache=shared" for an ephemeral
// database.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite serializes writes; one connection avoids lock churn and keeps a
	// shared in-memory DB coherent.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// NewWithDB wraps an existing *sql.DB (e.g. one with custom pragmas) and
// ensures the schema exists. The caller owns the DB lifecycle.
func NewWithDB(ctx context.Context, db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.init(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS links (
	short_id   TEXT PRIMARY KEY,
	type       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT,
	deleted_at TEXT,
	clicks     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_links_type ON links(type, created_at, short_id);
CREATE TABLE IF NOT EXISTS click_events (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	short_id TEXT NOT NULL,
	platform TEXT,
	referrer TEXT,
	day      TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_short ON click_events(short_id);
`

func (s *Store) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// Save upserts a link. On conflict it refreshes the mutable columns and
// clears deleted_at (a re-save resurrects a soft-deleted link) while
// preserving the original created_at and the accumulated click count.
func (s *Store) Save(ctx context.Context, id string, payload *deeplink.Link) error {
	payload.ShortID = id
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	const q = `
INSERT INTO links (short_id, type, payload, created_at, expires_at, deleted_at, clicks)
VALUES (?, ?, ?, ?, ?, NULL, 0)
ON CONFLICT(short_id) DO UPDATE SET
	type       = excluded.type,
	payload    = excluded.payload,
	expires_at = excluded.expires_at,
	deleted_at = NULL`

	createdAt := payload.CreatedAt
	if createdAt == "" {
		createdAt = nowUTC()
	}
	if _, err := s.db.ExecContext(ctx, q, id, payload.Type, string(data), createdAt, nullable(payload.ExpiresAt)); err != nil {
		return fmt.Errorf("save payload: %w", err)
	}
	return nil
}

// Get returns a link, transparently purging it when expired or when its
// soft-delete grace window has elapsed. Soft-deleted links still within the
// grace window are returned with DeletedAt set; callers decide what to do.
func (s *Store) Get(ctx context.Context, id string) (*deeplink.Link, error) {
	var data string
	var expiresAt, deletedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT payload, expires_at, deleted_at FROM links WHERE short_id = ?`, id).
		Scan(&data, &expiresAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deeplink.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get payload: %w", err)
	}

	now := time.Now()
	if expiresAt.Valid {
		if exp, perr := time.Parse(time.RFC3339, expiresAt.String); perr == nil && !exp.After(now) {
			s.purge(ctx, id)
			return nil, deeplink.ErrNotFound
		}
	}
	if deletedAt.Valid {
		if del, perr := time.Parse(time.RFC3339, deletedAt.String); perr == nil && now.Sub(del) >= deeplink.DeleteGracePeriod {
			s.purge(ctx, id)
			return nil, deeplink.ErrNotFound
		}
	}

	var payload deeplink.Link
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.ShortID == "" {
		payload.ShortID = id
	}
	return &payload, nil
}

// Delete soft-deletes a link by stamping DeletedAt in both the column and
// the stored payload. Returns [deeplink.ErrNotFound] if it is missing or
// already soft-deleted.
func (s *Store) Delete(ctx context.Context, id string) error {
	payload, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if payload.DeletedAt != "" {
		return deeplink.ErrNotFound
	}

	payload.DeletedAt = nowUTC()
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE links SET payload = ?, deleted_at = ? WHERE short_id = ?`, string(data), payload.DeletedAt, id); err != nil {
		return fmt.Errorf("soft-delete payload: %w", err)
	}
	return nil
}

func (s *Store) purge(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM links WHERE short_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM click_events WHERE short_id = ?`, id)
}

// IncrClickBy adds delta to the click count and returns the new value. A
// missing row (link expired or purged between resolve and flush) yields 0
// without error.
func (s *Store) IncrClickBy(ctx context.Context, id string, delta int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `UPDATE links SET clicks = clicks + ? WHERE short_id = ? RETURNING clicks`, delta, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("incr click: %w", err)
	}
	return n, nil
}

// Clicks returns the current click count, or 0 if the link is missing.
func (s *Store) Clicks(ctx context.Context, id string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT clicks FROM links WHERE short_id = ?`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get clicks: %w", err)
	}
	return n, nil
}

// List returns a page of live (non-deleted, non-expired) links of linkType,
// ordered by creation. The cursor is an opaque offset; the returned cursor
// is 0 when no more rows remain.
func (s *Store) List(ctx context.Context, linkType string, cursor uint64, count int64) ([]deeplink.LinkResponse, uint64, error) {
	if count <= 0 {
		count = 100
	}

	const q = `
SELECT payload, clicks FROM links
WHERE type = ?
  AND deleted_at IS NULL
  AND (expires_at IS NULL OR expires_at > ?)
ORDER BY created_at, short_id
LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, q, linkType, nowUTC(), count, int64(cursor))
	if err != nil {
		return nil, 0, fmt.Errorf("list links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	links := make([]deeplink.LinkResponse, 0, count)
	for rows.Next() {
		var data string
		var clicks int64
		if err := rows.Scan(&data, &clicks); err != nil {
			return nil, 0, fmt.Errorf("scan link: %w", err)
		}
		var p deeplink.Link
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			continue
		}
		links = append(links, deeplink.LinkResponse{Link: &p, ShortLink: p.ShortID, Clicks: clicks})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate links: %w", err)
	}

	var next uint64
	if int64(len(links)) == count {
		next = cursor + uint64(count)
	}
	return links, next, nil
}

// RecordEvents appends click events in a single transaction.
func (s *Store) RecordEvents(ctx context.Context, events []deeplink.ClickEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO click_events (short_id, platform, referrer, day) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, ev := range events {
		day := ev.Time.UTC().Format(time.DateOnly)
		if _, err := stmt.ExecContext(ctx, ev.ShortID, nullable(ev.Platform), nullable(ev.Referrer), day); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit events: %w", err)
	}
	return nil
}

// Stats aggregates click breakdowns for a link. The total click count comes
// from the links table; the breakdowns come from recorded events.
func (s *Store) Stats(ctx context.Context, shortID string) (deeplink.Stats, error) {
	out := deeplink.Stats{ShortID: shortID}

	clicks, err := s.Clicks(ctx, shortID)
	if err != nil {
		return out, err
	}
	out.Clicks = clicks

	out.ByPlatform, err = s.groupBy(ctx, "platform", shortID)
	if err != nil {
		return out, err
	}
	out.ByReferrer, err = s.groupBy(ctx, "referrer", shortID)
	if err != nil {
		return out, err
	}
	out.ByDay, err = s.groupBy(ctx, "day", shortID)
	if err != nil {
		return out, err
	}
	return out, nil
}

// groupBy counts events for shortID grouped by a fixed column. The column is
// interpolated into the SQL, so it is guarded against an allowlist: only the
// known breakdown columns are ever accepted, never user input.
func (s *Store) groupBy(ctx context.Context, column, shortID string) (map[string]int64, error) {
	switch column {
	case "platform", "referrer", "day":
	default:
		return nil, fmt.Errorf("groupBy: unsupported column %q", column)
	}

	q := fmt.Sprintf(`SELECT %s, COUNT(*) FROM click_events WHERE short_id = ? AND %s IS NOT NULL AND %s <> '' GROUP BY %s`, column, column, column, column)
	rows, err := s.db.QueryContext(ctx, q, shortID)
	if err != nil {
		return nil, fmt.Errorf("group by %s: %w", column, err)
	}
	defer func() { _ = rows.Close() }()

	var out map[string]int64
	for rows.Next() {
		var key string
		var n int64
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		if out == nil {
			out = make(map[string]int64)
		}
		out[key] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups: %w", err)
	}
	return out, nil
}
