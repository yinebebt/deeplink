package deeplink

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ListCap bounds a [Service.List] across all types.
const ListCap = 500

// Create runs the processor for link.Type, stores the link, and returns its
// short URL.
func (s *Service) Create(ctx context.Context, link *Link) (string, error) {
	processor := s.registry.Get(link.Type)
	if processor == nil {
		return "", ErrInvalidType
	}
	if err := processor.Process(ctx, link); err != nil {
		return "", err
	}

	shortURL, err := s.shortenURL(ctx, link)
	if err != nil {
		if _, ok := errors.AsType[*Error](err); ok {
			return "", err
		}
		return "", NewError(err, http.StatusInternalServerError, "failed to shorten URL")
	}
	return shortURL, nil
}

// Get returns a link with its short URL and click count. A soft-deleted link
// is [ErrNotFound].
func (s *Service) Get(ctx context.Context, shortID string) (LinkResponse, error) {
	shortID = strings.TrimPrefix(shortID, s.config.BaseURL)

	link, err := s.config.Store.Get(ctx, shortID)
	if err != nil || link.DeletedAt != "" {
		return LinkResponse{}, ErrNotFound
	}

	clicks, _ := s.config.Store.Clicks(ctx, shortID)
	return LinkResponse{
		Link:      link,
		ShortLink: s.config.BaseURL + shortID,
		Clicks:    clicks,
	}, nil
}

// List returns links of linkType, or of every type when linkType is empty.
// limit <= 0 means no limit for one type and [ListCap] across all types.
func (s *Service) List(ctx context.Context, linkType string, limit int) ([]LinkResponse, error) {
	if linkType != "" {
		return s.listLinksByType(ctx, linkType, limit)
	}

	if limit <= 0 || limit > ListCap {
		limit = ListCap
	}
	out := make([]LinkResponse, 0)
	for _, t := range s.registry.Types() {
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		batch, err := s.listLinksByType(ctx, t, remaining)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// Stats returns click analytics for a link. A soft-deleted link is
// [ErrNotFound].
func (s *Service) Stats(ctx context.Context, shortID string) (Stats, error) {
	shortID = strings.TrimPrefix(shortID, s.config.BaseURL)

	link, err := s.config.Store.Get(ctx, shortID)
	if err != nil || link.DeletedAt != "" {
		return Stats{}, ErrNotFound
	}
	return s.config.Store.Stats(ctx, shortID)
}

// Delete soft-deletes a link; it is purged after [DeleteGracePeriod].
func (s *Service) Delete(ctx context.Context, shortID string) error {
	shortID = strings.TrimPrefix(shortID, s.config.BaseURL)
	return s.config.Store.Delete(ctx, shortID)
}
