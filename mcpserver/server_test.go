package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yinebebt/deeplink"
)

func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	svc, err := deeplink.New(deeplink.Config{
		BaseURL: "https://example.com/",
		Store:   deeplink.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Register(deeplink.RedirectProcessor{})
	t.Cleanup(func() { _ = svc.Close() })

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := New(svc).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// call runs a tool and decodes its structured output into T.
func call[T any](t *testing.T, s *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()

	res := callRaw(t, s, name, args)
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decode %s: %v", name, raw, err)
	}
	return out
}

func callRaw(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestLinkLifecycle(t *testing.T) {
	t.Parallel()
	s := newSession(t)

	created := call[createLinkOutput](t, s, "create_link", map[string]any{
		"url":   "https://go.dev/",
		"title": "Go",
	})
	if !strings.HasPrefix(created.ShortURL, "https://example.com/") {
		t.Fatalf("short_url = %q, want the base URL prefix", created.ShortURL)
	}

	shortID := strings.TrimPrefix(created.ShortURL, "https://example.com/")
	link := call[deeplink.LinkResponse](t, s, "get_link", map[string]any{"short_id": created.ShortURL})
	if link.URL != "https://go.dev/" || link.Type != "redirect" || link.ShortID != shortID {
		t.Fatalf("get_link = %+v, want the created redirect link", link)
	}

	list := call[listLinksOutput](t, s, "list_links", nil)
	if len(list.Links) != 1 || list.Links[0].ShortLink != created.ShortURL {
		t.Fatalf("list_links = %+v, want the one created link", list.Links)
	}

	stats := call[deeplink.Stats](t, s, "link_stats", map[string]any{"short_id": shortID})
	if stats.ShortID != shortID || stats.Clicks != 0 {
		t.Fatalf("link_stats = %+v, want zero clicks for %s", stats, shortID)
	}

	call[deleteLinkOutput](t, s, "delete_link", map[string]any{"short_id": shortID})
	if res := callRaw(t, s, "get_link", map[string]any{"short_id": shortID}); !res.IsError || text(res) != "link not found" {
		t.Fatalf("get_link after delete: IsError=%v text=%q, want link not found", res.IsError, text(res))
	}
}

func TestCreateLinkRejectsBadInput(t *testing.T) {
	t.Parallel()
	s := newSession(t)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"non-http scheme", map[string]any{"url": "javascript:alert(1)"}, "url must use http or https"},
		{"unknown type", map[string]any{"url": "https://go.dev/", "type": "nope"}, "invalid link type"},
		{"past expiry", map[string]any{"url": "https://go.dev/", "expires_at": "2000-01-01T00:00:00Z"}, "expires_at must be in the future"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callRaw(t, s, "create_link", tc.args)
			if !res.IsError || text(res) != tc.want {
				t.Fatalf("IsError=%v text=%q, want %q", res.IsError, text(res), tc.want)
			}
		})
	}
}

func TestListTypes(t *testing.T) {
	t.Parallel()
	s := newSession(t)

	out := call[listTypesOutput](t, s, "list_types", nil)
	if len(out.Types) != 1 || out.Types[0] != "redirect" {
		t.Fatalf("list_types = %v, want [redirect]", out.Types)
	}
}
