package deeplink

import (
	"context"
	"testing"
)

func TestRedirectProcessorProcess(t *testing.T) {
	t.Parallel()

	processor := RedirectProcessor{}

	t.Run("uses payload URL directly", func(t *testing.T) {
		t.Parallel()

		payload := &Link{URL: "https://example.com/direct"}
		if err := processor.Process(context.Background(), payload); err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		if payload.URL != "https://example.com/direct" {
			t.Fatalf("payload.URL = %q", payload.URL)
		}
		if payload.Title == "" {
			t.Fatal("expected default title to be set")
		}
	})

	t.Run("uses metadata url", func(t *testing.T) {
		t.Parallel()

		payload := &Link{
			Metadata: map[string]any{
				"url": "https://example.com/from-metadata",
			},
		}
		if err := processor.Process(context.Background(), payload); err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		if payload.URL != "https://example.com/from-metadata" {
			t.Fatalf("payload.URL = %q", payload.URL)
		}
	})

	t.Run("rejects missing url", func(t *testing.T) {
		t.Parallel()

		payload := &Link{}
		if err := processor.Process(context.Background(), payload); err == nil {
			t.Fatal("expected error for missing url")
		}
	})
}
