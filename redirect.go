package deeplink

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// RedirectProcessor handles generic URL redirects.
// It reads the destination from payload.URL or Meta("url").
type RedirectProcessor struct{}

func (RedirectProcessor) Type() string {
	return "redirect"
}

func (RedirectProcessor) Process(_ context.Context, payload *Link) error {
	target := payload.URL
	if target == "" {
		target = payload.Meta("url")
	}

	if target == "" {
		return NewError(fmt.Errorf("missing redirect URL"), http.StatusBadRequest, "url is required")
	}
	u, err := url.ParseRequestURI(target)
	if err != nil {
		return NewError(err, http.StatusBadRequest, "url must be a valid absolute URL")
	}
	// Restrict to http(s): the preview page assigns the destination to
	// window.location.href, so a javascript:/data: URL would be stored XSS.
	// Universal/App Links are https; custom schemes need a dedicated processor.
	if u.Scheme != "http" && u.Scheme != "https" {
		return NewError(fmt.Errorf("disallowed url scheme %q", u.Scheme), http.StatusBadRequest, "url must use http or https")
	}

	payload.URL = target
	if payload.Title == "" {
		payload.Title = "Open link"
	}
	if payload.Description == "" {
		payload.Description = target
	}

	return nil
}
