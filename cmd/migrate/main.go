// Command migrate imports a deeplink NDJSON backup into a SQLite store.
// Each input line is one record: {"id":"..","clicks":N,"payload":{<Link>}}.
// Used to move Redis-era link data onto the SQLite backend; idempotent
// (re-running upserts the same IDs).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/yinebebt/deeplink"
	"github.com/yinebebt/deeplink/sqlitestore"
)

type record struct {
	ID      string        `json:"id"`
	Clicks  int64         `json:"clicks"`
	Payload deeplink.Link `json:"payload"`
}

func main() {
	in := flag.String("in", "", "NDJSON backup file (one record per line)")
	dsn := flag.String("out", "deeplink.db", "SQLite DSN / file path")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: migrate -in backup.ndjson -out deeplink.db")
		os.Exit(2)
	}
	if err := run(*in, *dsn); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(in, dsn string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	store, err := sqlitestore.New(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	var links, clicks int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("parse line %d: %w", links+1, err)
		}
		id := rec.ID
		if id == "" {
			id = rec.Payload.ShortID
		}
		if err := store.Save(ctx, id, &rec.Payload); err != nil {
			return fmt.Errorf("save %s: %w", id, err)
		}
		if rec.Clicks > 0 {
			if _, err := store.IncrClickBy(ctx, id, rec.Clicks); err != nil {
				return fmt.Errorf("clicks %s: %w", id, err)
			}
			clicks += int(rec.Clicks)
		}
		links++
	}
	if err := sc.Err(); err != nil {
		return err
	}

	fmt.Printf("migrated %d links, %d total clicks -> %s\n", links, clicks, dsn)
	return nil
}
