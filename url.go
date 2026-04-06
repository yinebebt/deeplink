package deeplink

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
)

// shortenURL generates a short ID, stores the payload, and returns the full short URL.
const maxIDRetries = 100

func (s *Service) shortenURL(ctx context.Context, payload *Link) (string, error) {
	var id string
	var err error

	for range maxIDRetries {
		id, err = nanoid.New(s.config.IDLength)
		if err != nil {
			return "", fmt.Errorf("generate nano ID: %w", err)
		}
		if !s.skipPath(id) {
			break
		}
		id = ""
	}

	if id == "" {
		return "", fmt.Errorf("failed to generate ID after %d attempts (check SkipPaths)", maxIDRetries)
	}

	payload.ShortID = id
	payload.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.config.Store.Save(ctx, id, payload); err != nil {
		return "", fmt.Errorf("store payload: %w", err)
	}

	s.config.Logger.Info("URL shortened", "shortID", id, "type", payload.Type)
	return s.config.BaseURL + id, nil
}

// expandURL looks up a short ID, increments clicks, and returns the payload.
func (s *Service) expandURL(ctx context.Context, shortID string) (*Link, error) {
	shortID = strings.TrimPrefix(shortID, s.config.BaseURL)

	payload, err := s.config.Store.Get(ctx, shortID)
	if err != nil {
		return nil, fmt.Errorf("expand URL %s: %w", shortID, err)
	}

	s.clicks.track(shortID)

	return payload, nil
}

func (s *Service) skipPath(path string) bool {
	for _, re := range s.skipPatterns {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

func compileSkipPatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile skip pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}
