package deeplink

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Config configures a deeplink [Service].
type Config struct {
	// BaseURL is the prefix for generated short links
	// (e.g. "https://link.example.com" or "https://link.example.com/").
	// A trailing slash is added automatically if missing.
	BaseURL string

	// IDLength is the length of generated short IDs. Default: 17.
	IDLength int

	// DefaultEnvironment is used when no environment is specified
	// in the request. Default: "dev".
	DefaultEnvironment string

	// Store is the persistence backend for links. Required.
	Store Store

	// Logger for structured logging. Default: [slog.Default].
	Logger *slog.Logger

	// HTTPClient is available to processors via [Service.Config].
	// Default: client with 20s timeout.
	HTTPClient *http.Client

	// SkipPaths are regex patterns matched against incoming short IDs.
	// Matching requests get a 404 instead of a store lookup.
	SkipPaths []string

	// TemplateDir is the directory containing link.html and preview.html.
	// If empty or if a template file is missing, the preview handler
	// falls back to a 302 redirect.
	TemplateDir string

	// AllowedOrigins for CORS. Empty means no CORS headers.
	AllowedOrigins []string

	// AndroidStoreURL, IOSStoreURL, and WebFallbackURL enable the
	// /redirect, /preview/, and /.well-known/ routes when any is set.
	AndroidStoreURL string
	IOSStoreURL     string
	WebFallbackURL  string
}

func (c *Config) defaults() {
	if c.IDLength == 0 {
		c.IDLength = 17
	}
	if c.DefaultEnvironment == "" {
		c.DefaultEnvironment = "dev"
	}
	if c.BaseURL != "" && !strings.HasSuffix(c.BaseURL, "/") {
		c.BaseURL += "/"
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 20 * time.Second,
		}
	}
}

func (c *Config) hasMobileRoutes() bool {
	return c.AndroidStoreURL != "" || c.IOSStoreURL != "" || c.WebFallbackURL != ""
}
