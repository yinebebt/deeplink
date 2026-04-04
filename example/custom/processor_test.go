package main

import (
	"context"
	"strings"
	"testing"

	"github.com/yinebebt/deeplink"
)

func TestTelegramProcessor(t *testing.T) {
	t.Parallel()

	processor := telegramProcessor{}

	t.Run("generates bot start link", func(t *testing.T) {
		t.Parallel()

		payload := &deeplink.Link{
			Type: "telegram",
			Metadata: map[string]any{
				"bot_name": "mybot",
				"start":    "group_abc123",
			},
		}
		if err := processor.Process(context.Background(), payload); err != nil {
			t.Fatalf("Process() error = %v", err)
		}

		if !strings.Contains(payload.URL, "t.me/mybot") {
			t.Fatalf("URL = %q, want t.me bot link", payload.URL)
		}
		if !strings.Contains(payload.URL, "start=group_abc123") {
			t.Fatalf("URL = %q, want start param", payload.URL)
		}
	})

	t.Run("passes extra metadata as query params", func(t *testing.T) {
		t.Parallel()

		payload := &deeplink.Link{
			Type: "telegram",
			Metadata: map[string]any{
				"bot_name": "mybot",
				"start":    "ref_abc",
				"sharer":   "user-1",
				"campaign": "spring",
			},
		}
		if err := processor.Process(context.Background(), payload); err != nil {
			t.Fatalf("Process() error = %v", err)
		}

		if !strings.Contains(payload.URL, "sharer=user-1") {
			t.Fatalf("URL = %q, want sharer param", payload.URL)
		}
		if !strings.Contains(payload.URL, "campaign=spring") {
			t.Fatalf("URL = %q, want campaign param", payload.URL)
		}
	})

	t.Run("rejects missing bot name", func(t *testing.T) {
		t.Parallel()

		payload := &deeplink.Link{Metadata: map[string]any{"start": "x"}}
		if err := processor.Process(context.Background(), payload); err == nil {
			t.Fatal("expected error for missing bot_name")
		}
	})

	t.Run("rejects missing start", func(t *testing.T) {
		t.Parallel()

		payload := &deeplink.Link{Metadata: map[string]any{"bot_name": "mybot"}}
		if err := processor.Process(context.Background(), payload); err == nil {
			t.Fatal("expected error for missing start")
		}
	})

	t.Run("sets default title and description", func(t *testing.T) {
		t.Parallel()

		payload := &deeplink.Link{
			Type: "telegram",
			Metadata: map[string]any{
				"bot_name": "mybot",
				"start":    "123",
			},
		}
		if err := processor.Process(context.Background(), payload); err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		if payload.Title != "Open in Telegram" {
			t.Fatalf("Title = %q, want default", payload.Title)
		}
		if !strings.Contains(payload.Description, "@mybot") {
			t.Fatalf("Description = %q, want bot mention", payload.Description)
		}
	})

	t.Run("custom link type", func(t *testing.T) {
		t.Parallel()

		p := telegramProcessor{linkType: "bot-invite"}
		if p.Type() != "bot-invite" {
			t.Fatalf("Type() = %q, want %q", p.Type(), "bot-invite")
		}
	})
}
