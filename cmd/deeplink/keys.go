package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/yinebebt/keyring"
)

// seedAPIKey copies DEEPLINK_API_KEY into an empty store so existing
// deployments keep their key; after that the store wins.
func seedAPIKey(ctx context.Context, store keyring.Store, apiKey string, logger *slog.Logger) error {
	ks, err := store.Get(ctx)
	if err != nil {
		return fmt.Errorf("read API keys: %w", err)
	}
	switch {
	case ks.Current == "" && apiKey != "":
		return store.Put(ctx, keyring.KeySet{Current: apiKey})
	case ks.Current == "":
		logger.Warn("no API key: protected routes return 401 until DEEPLINK_API_KEY is set or `deeplink keys rotate` runs")
	case apiKey != "" && apiKey != ks.Current && apiKey != ks.Previous:
		logger.Warn("DEEPLINK_API_KEY ignored: the store holds a rotated key")
	}
	return nil
}

// runKeys implements `deeplink keys rotate [-grace 24h] | revoke | status`.
// rotate prints the new key on stdout; every command prints the state on stderr.
func runKeys(args []string) error {
	usage := errors.New("usage: deeplink keys rotate [-grace 24h] | revoke | status")
	if len(args) == 0 {
		return usage
	}
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	grace := fs.Duration("grace", 24*time.Hour, "how long the old key stays valid after rotate")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if env("DEEPLINK_STORE", "sqlite") == "memory" {
		return errors.New("the memory store lives inside the server process; nothing to rotate")
	}

	ctx := context.Background()
	_, store, closeStore, err := buildStore(ctx, slog.New(slog.DiscardHandler))
	if err != nil {
		return err
	}
	defer closeStore()
	kr := keyring.New(store, *grace)

	var ks keyring.KeySet
	switch args[0] {
	case "rotate":
		ks, err = kr.Rotate(ctx)
	case "revoke":
		ks, err = kr.Revoke(ctx)
	case "status":
		ks, err = kr.Status(ctx)
	default:
		return usage
	}
	if err != nil {
		return err
	}
	if args[0] == "rotate" {
		fmt.Println(ks.Current)
	}
	state := ks.State(time.Now())
	if state == keyring.StateGrace {
		fmt.Fprintf(os.Stderr, "%s: old key valid until %s\n", state, ks.GraceUntil.Format(time.RFC3339))
	} else {
		fmt.Fprintln(os.Stderr, state)
	}
	return nil
}
