package redisstore

import (
	"testing"
	"time"

	"github.com/yinebebt/deeplink"
)

func TestPayloadTTL(t *testing.T) {
	t.Parallel()
	if got := payloadTTL(&deeplink.Link{}); got != 0 {
		t.Fatalf("empty ExpiresAt = %v, want 0", got)
	}
	if got := payloadTTL(&deeplink.Link{ExpiresAt: "not-a-date"}); got != 0 {
		t.Fatalf("unparseable = %v, want 0", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := payloadTTL(&deeplink.Link{ExpiresAt: past}); got != 0 {
		t.Fatalf("past = %v, want 0", got)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if got := payloadTTL(&deeplink.Link{ExpiresAt: future}); got <= 0 {
		t.Fatalf("future = %v, want positive", got)
	}
}
