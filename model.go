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
	ImageURL string `json:"image_url,omitempty"`

	// Environment groups links (e.g. "dev", "staging", "production").
	Environment string `json:"environment,omitempty"`
	// CreatedAt is an RFC 3339 timestamp. Set by the service.
	CreatedAt string `json:"created_at,omitempty"`

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

// LinkInfo is the response type for link list and detail endpoints.
type LinkInfo struct {
	ShortLink string `json:"short_link"`
	URL       string `json:"url"`
	Clicks    int64  `json:"clicks"`
}
