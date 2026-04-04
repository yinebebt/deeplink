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
	if _, err := url.ParseRequestURI(target); err != nil {
		return NewError(err, http.StatusBadRequest, "url must be a valid absolute URL")
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
