package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/yinebebt/deeplink"
)

// telegramProcessor builds https://t.me/{bot}?start={param} URLs.
//
// Required metadata:
//   - "bot_name": Telegram bot username (without @)
//   - "start": the start parameter value
//
// Any additional metadata keys are appended as query parameters.
type telegramProcessor struct {
	// linkType overrides the type identifier (default: "telegram").
	linkType string
}

func (p telegramProcessor) Type() string {
	if p.linkType != "" {
		return p.linkType
	}
	return "telegram"
}

func (p telegramProcessor) Process(_ context.Context, payload *deeplink.Link) error {
	botName := payload.Meta("bot_name")
	start := payload.Meta("start")

	if botName == "" {
		return deeplink.NewError(fmt.Errorf("missing bot_name"), http.StatusBadRequest, "metadata.bot_name is required")
	}
	if start == "" {
		return deeplink.NewError(fmt.Errorf("missing start"), http.StatusBadRequest, "metadata.start is required")
	}

	q := url.Values{}
	q.Set("start", start)
	for k, v := range payload.Metadata {
		if k == "bot_name" || k == "start" {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			q.Set(k, s)
		}
	}

	payload.URL = fmt.Sprintf("https://t.me/%s?%s", botName, q.Encode())

	if payload.Title == "" {
		payload.Title = "Open in Telegram"
	}
	if payload.Description == "" {
		payload.Description = fmt.Sprintf("Open @%s in Telegram", botName)
	}

	return nil
}
