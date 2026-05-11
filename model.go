package deeplink

// Link represents a deep link with its metadata.
//
// The core fields (Type, URL, Title, Description, ImageURL) drive the
// built-in preview templates. Processors use Metadata for everything else.
type Link struct {
	// Type identifies the link type (e.g. "redirect").
	Type string `json:"type,omitempty"`
	// ShortID is the generated identifier. Set by the service, not the caller.
	ShortID string `json:"short_id,omitempty"`

	// URL is the destination URL.
	URL string `json:"url,omitempty"`
	// Title for the OG preview page.
	Title string `json:"title,omitempty"`
	// Description for the OG preview page.
	Description string `json:"description,omitempty"`
	// ImageURL for the OG preview image.
	// PNG/JPG/WebP only (SVG fails most scrapers); 1200x630 recommended.
	ImageURL string `json:"image_url,omitempty"`
	// ImageWidth is the og:image:width hint in pixels.
	ImageWidth int `json:"image_width,omitempty"`
	// ImageHeight is the og:image:height hint in pixels.
	ImageHeight int `json:"image_height,omitempty"`
	// ImageAlt is the alt text for og:image and twitter:image.
	ImageAlt string `json:"image_alt,omitempty"`
	// OGType is the og:type value (e.g. "website", "article", "profile").
	// Empty defaults to "website" in the template. Setting "article" also
	// emits article:published_time and article:modified_time tags.
	OGType string `json:"og_type,omitempty"`
	// Locale overrides the per-link og:locale (e.g. "en_US", "fr_FR").
	// Empty falls back to Config.Locale.
	Locale string `json:"locale,omitempty"`

	// CreatedAt is an RFC 3339 timestamp. Set by the service.
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt is an RFC 3339 timestamp. Set by the service.
	UpdatedAt string `json:"updated_at,omitempty"`
	// DeletedAt is an RFC 3339 timestamp set when the link is soft-deleted.
	// Soft-deleted links are hidden from list and preview; the underlying
	// record is purged after [DeleteGracePeriod] elapses.
	DeletedAt string `json:"deleted_at,omitempty"`

	// Metadata holds processor-specific data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Meta returns a metadata value as a string, or "" if missing or not a string.
func (l *Link) Meta(key string) string {
	if l.Metadata == nil {
		return ""
	}
	s, _ := l.Metadata[key].(string)
	return s
}

// SetMeta sets a metadata key, initializing the map if needed.
func (l *Link) SetMeta(key string, value any) {
	if l.Metadata == nil {
		l.Metadata = make(map[string]any)
	}
	l.Metadata[key] = value
}

// LinkResponse is the wire shape returned by all link-reading endpoints
// (list, detail, update). It embeds the stored [Link] and adds the
// rendered short URL plus the current click count.
//
// At the store layer, ShortLink carries only the short ID; HTTP handlers
// prefix it with [Config.BaseURL] before responding so callers see a full
// URL.
type LinkResponse struct {
	*Link
	ShortLink string `json:"short_link"`
	Clicks    int64  `json:"clicks"`
}
