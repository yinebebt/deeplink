package deeplink

import (
	"net/http"
	"strings"
)

// previewCacheControl is the Cache-Control header for rendered preview pages.
// Short max-age lets caller metadata edits propagate through HTTP caches
// within minutes; the SWR window keeps responses fast in the meantime.
const previewCacheControl = "public, max-age=300, stale-while-revalidate=600"

// previewView is the data passed to the default templates. It embeds *Link
// so existing template fields keep working, and adds config-derived values
// the templates need but the Link itself does not carry.
//
// OGType, Locale, and Lang shadow the corresponding embedded Link fields on
// purpose: the wrapper applies the Config.Locale fallback, lowercases OGType
// so the template's exact-string comparison stays consistent, and derives
// Lang from Locale.
//
// Custom processors that implement Previewer and return non-nil data bypass
// this wrapper; their data is responsible for the fields its template uses.
type previewView struct {
	*Link
	ShortURL         string
	SiteName         string
	Locale           string
	Lang             string
	OGType           string
	TwitterSite      string
	FediverseCreator string
}

// buildPreviewData wraps a Link with the config-derived rendering data the
// default templates expect. Custom Previewer output, when non-nil, is passed
// through unchanged.
func (s *Service) buildPreviewData(payload *Link) any {
	processor := s.registry.Get(payload.Type)
	if p, ok := processor.(Previewer); ok {
		if data := p.Preview(payload); data != nil {
			return data
		}
	}

	locale := payload.Locale
	if locale == "" {
		locale = s.config.Locale
	}

	return previewView{
		Link:             payload,
		ShortURL:         s.config.BaseURL + payload.ShortID,
		SiteName:         s.config.SiteName,
		Locale:           locale,
		Lang:             langFromLocale(locale),
		OGType:           strings.ToLower(strings.TrimSpace(payload.OGType)),
		TwitterSite:      s.config.TwitterSite,
		FediverseCreator: s.config.FediverseCreator,
	}
}

// langFromLocale extracts the BCP 47 language subtag from an OG locale value
// (e.g. "en_US" -> "en", "fr-FR" -> "fr"). Returns "en" as a safe fallback.
func langFromLocale(locale string) string {
	if locale == "" {
		return "en"
	}
	if i := strings.IndexAny(locale, "_-"); i > 0 {
		return strings.ToLower(locale[:i])
	}
	return strings.ToLower(locale)
}

// writePreviewHTML writes a rendered preview page with the shared
// Content-Type and Cache-Control headers used by both preview handlers.
func writePreviewHTML(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", previewCacheControl)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
