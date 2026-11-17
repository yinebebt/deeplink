// Package mcpserver exposes a [deeplink.Service] as Model Context Protocol
// tools. The handler does not authenticate; put it behind an API key.
package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yinebebt/deeplink"
)

const defaultListLimit = 50

type (
	tools struct {
		svc    *deeplink.Service
		logger *slog.Logger
	}

	createLinkInput struct {
		URL         string         `json:"url,omitempty" jsonschema:"destination URL (http or https); required by the redirect type"`
		Type        string         `json:"type,omitempty" jsonschema:"link type from list_types; defaults to redirect"`
		Title       string         `json:"title,omitempty" jsonschema:"title for the preview card"`
		Description string         `json:"description,omitempty" jsonschema:"description for the preview card"`
		ImageURL    string         `json:"image_url,omitempty" jsonschema:"preview image URL: PNG, JPG, or WebP, ideally 1200x630"`
		ExpiresAt   string         `json:"expires_at,omitempty" jsonschema:"RFC 3339 time in the future after which the link stops resolving"`
		Metadata    map[string]any `json:"metadata,omitempty" jsonschema:"fields specific to the link type"`
	}
	createLinkOutput struct {
		ShortURL string `json:"short_url"`
	}

	linkRef struct {
		ShortID string `json:"short_id" jsonschema:"short ID, or the full short URL"`
	}

	listLinksInput struct {
		Type  string `json:"type,omitempty" jsonschema:"only links of this type; all types when empty"`
		Limit int    `json:"limit,omitempty" jsonschema:"maximum number of links to return; default 50"`
	}
	listLinksOutput struct {
		Links []deeplink.LinkResponse `json:"links"`
	}

	deleteLinkOutput struct {
		ShortID    string `json:"short_id"`
		PurgeAfter string `json:"purge_after" jsonschema:"how long the record is kept before it is purged"`
	}

	listTypesOutput struct {
		Types []string `json:"types"`
	}
)

// New returns an MCP server with tools backed by svc.
func New(svc *deeplink.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "deeplink", Version: version()}, nil)
	t := &tools{svc: svc, logger: svc.Config().Logger}

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	notDestructive := false

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_link",
		Description: "Create a short link with an optional preview card and expiry. Returns the short URL.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
	}, t.createLink)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_link",
		Description: "Get a link's destination, preview fields, and click count.",
		Annotations: readOnly,
	}, t.getLink)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_links",
		Description: "List links with their click counts, optionally filtered by type.",
		Annotations: readOnly,
	}, t.listLinks)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "link_stats",
		Description: "Get click analytics for a link: total, and breakdowns by platform, referrer, and day.",
		Annotations: readOnly,
	}, t.linkStats)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_link",
		Description: "Delete a link. It stops resolving at once and is purged after a grace period.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, t.deleteLink)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_types",
		Description: "List the link types this server accepts in create_link.",
		Annotations: readOnly,
	}, t.listTypes)

	return server
}

// Handler serves the tools over streamable HTTP. A nil opts means stateless
// with JSON responses.
func Handler(svc *deeplink.Service, opts *mcp.StreamableHTTPOptions) http.Handler {
	if opts == nil {
		opts = &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}
	}
	server := New(svc)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, opts)
}

func (t *tools) createLink(ctx context.Context, _ *mcp.CallToolRequest, in createLinkInput) (*mcp.CallToolResult, createLinkOutput, error) {
	link := &deeplink.Link{
		Type:        in.Type,
		URL:         in.URL,
		Title:       in.Title,
		Description: in.Description,
		ImageURL:    in.ImageURL,
		ExpiresAt:   in.ExpiresAt,
		Metadata:    in.Metadata,
	}
	if link.Type == "" {
		link.Type = deeplink.RedirectProcessor{}.Type()
	}

	shortURL, err := t.svc.Create(ctx, link)
	if err != nil {
		return nil, createLinkOutput{}, t.toolError("create_link", err)
	}
	return nil, createLinkOutput{ShortURL: shortURL}, nil
}

func (t *tools) getLink(ctx context.Context, _ *mcp.CallToolRequest, in linkRef) (*mcp.CallToolResult, deeplink.LinkResponse, error) {
	link, err := t.svc.Get(ctx, in.ShortID)
	if err != nil {
		return nil, deeplink.LinkResponse{}, t.toolError("get_link", err)
	}
	return nil, link, nil
}

func (t *tools) listLinks(ctx context.Context, _ *mcp.CallToolRequest, in listLinksInput) (*mcp.CallToolResult, listLinksOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	links, err := t.svc.List(ctx, in.Type, limit)
	if err != nil {
		return nil, listLinksOutput{}, t.toolError("list_links", err)
	}
	return nil, listLinksOutput{Links: links}, nil
}

func (t *tools) linkStats(ctx context.Context, _ *mcp.CallToolRequest, in linkRef) (*mcp.CallToolResult, deeplink.Stats, error) {
	stats, err := t.svc.Stats(ctx, in.ShortID)
	if err != nil {
		return nil, deeplink.Stats{}, t.toolError("link_stats", err)
	}
	return nil, stats, nil
}

func (t *tools) deleteLink(ctx context.Context, _ *mcp.CallToolRequest, in linkRef) (*mcp.CallToolResult, deleteLinkOutput, error) {
	if err := t.svc.Delete(ctx, in.ShortID); err != nil {
		return nil, deleteLinkOutput{}, t.toolError("delete_link", err)
	}
	return nil, deleteLinkOutput{ShortID: in.ShortID, PurgeAfter: deeplink.DeleteGracePeriod.String()}, nil
}

func (t *tools) listTypes(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, listTypesOutput, error) {
	return nil, listTypesOutput{Types: t.svc.Types()}, nil
}

// toolError passes caller mistakes (4xx) through and hides internal failures.
func (t *tools) toolError(tool string, err error) error {
	if appErr, ok := errors.AsType[*deeplink.Error](err); ok && appErr.Code < http.StatusInternalServerError {
		return errors.New(appErr.Message)
	}
	t.logger.Error("mcp tool failed", "tool", tool, "error", err)
	return errors.New("internal error")
}

// version reports the deeplink module version from the build info.
func version() string {
	const module = "github.com/yinebebt/deeplink"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Path == module && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == module {
			return dep.Version
		}
	}
	return "dev"
}
