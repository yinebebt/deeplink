package deeplink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// Handler returns an [http.Handler] with the following routes:
//
//	POST   /shorten              create a short link
//	PATCH  /{shortID}            update mutable fields on an existing link
//	DELETE /{shortID}            soft-delete a link (grace = [DeleteGracePeriod])
//	GET    /{shortID}            preview page (or 302 if no template)
//	GET    /links                full link list across all types (dashboard)
//	GET    /links/{type}         list links by type
//	GET    /links/{type}/{shortID} single link with click count
//	GET    /types                registered processor type names
//	GET    /health               health check
//
// When store URL fields are set in [Config], these are also registered:
//
//	GET  /redirect          platform-aware app store redirect
//	GET  /preview/{shortID}      preview without auto-redirect
//	GET  /.well-known/           static files from TemplateDir
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /shorten", s.handleGenerate)
	mux.HandleFunc("GET /links", s.handleLinkListAll)
	mux.HandleFunc("GET /links/{type}", s.handleLinkList)
	mux.HandleFunc("GET /links/{type}/{shortID}", s.handleLinkDetail)
	mux.HandleFunc("GET /types", s.handleTypes)
	mux.HandleFunc("GET /health", handleHealth)

	mux.HandleFunc("GET /stats/{shortID}", s.handleStats)

	if s.config.hasMobileRoutes() {
		mux.HandleFunc("GET /.well-known/", s.handleWellKnown)
		mux.HandleFunc("GET /redirect", s.handleRedirect)
		mux.HandleFunc("GET /preview/{shortID}", s.handleStaticPreview)
	}

	mux.HandleFunc("PATCH /{shortID}", s.handleUpdate)
	mux.HandleFunc("DELETE /{shortID}", s.handleDelete)
	mux.HandleFunc("GET /{shortID}", s.handlePreview)

	return s.withCORS(mux.ServeHTTP)
}

func (s *Service) handleGenerate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var payload Link
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.respondError(w, NewError(err, http.StatusBadRequest, "invalid JSON payload"))
		return
	}

	processor := s.registry.Get(payload.Type)
	if processor == nil {
		s.respondError(w, ErrInvalidType)
		return
	}

	if err := processor.Process(r.Context(), &payload); err != nil {
		s.respondError(w, err)
		return
	}

	shortURL, err := s.shortenURL(r.Context(), &payload)
	if err != nil {
		// Preserve a deliberate status (e.g. 400 for a bad expires_at);
		// wrap anything else as a 500.
		if appErr, ok := errors.AsType[*Error](err); ok {
			s.respondError(w, appErr)
			return
		}
		s.config.Logger.Error("failed to shorten URL", "error", err, "type", payload.Type)
		s.respondError(w, NewError(err, http.StatusInternalServerError, "failed to shorten URL"))
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"short_url": shortURL})
	s.config.Logger.Info("link generated", "shortID", strings.TrimPrefix(shortURL, s.config.BaseURL), "type", payload.Type, "duration", time.Since(start).String())
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	shortID := r.PathValue("shortID")
	if s.skipPath(shortID) {
		s.config.Logger.Debug("preview skipped (path filter)", "shortID", shortID)
		http.NotFound(w, r)
		return
	}

	payload, err := s.expandURL(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Warn("preview not found", "shortID", shortID, "error", err)
		http.NotFound(w, r)
		return
	}
	if payload.DeletedAt != "" {
		s.config.Logger.Info("preview hidden (soft-deleted)", "shortID", shortID, "deletedAt", payload.DeletedAt)
		http.NotFound(w, r)
		return
	}

	s.clicks.track(newClickEvent(r, shortID))

	tmpl := s.templates["link"]
	if tmpl == nil {
		http.Redirect(w, r, payload.URL, http.StatusFound)
		return
	}

	previewData := s.buildPreviewData(payload)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, previewData); err != nil {
		s.config.Logger.Error("template execution failed", "error", err, "shortID", shortID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writePreviewHTML(w, buf.Bytes())
	s.config.Logger.Info("preview rendered", "shortID", shortID, "type", payload.Type, "duration", time.Since(start).String())
}

// handleStaticPreview renders a preview page without auto-redirect.
// Useful as an intermediate page for iOS Universal Links or any
// flow where the user should tap to continue.
func (s *Service) handleStaticPreview(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	shortID := r.PathValue("shortID")

	payload, err := s.expandURL(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Warn("static preview not found", "shortID", shortID, "error", err)
		http.NotFound(w, r)
		return
	}
	if payload.DeletedAt != "" {
		s.config.Logger.Info("static preview hidden (soft-deleted)", "shortID", shortID, "deletedAt", payload.DeletedAt)
		http.NotFound(w, r)
		return
	}

	s.clicks.track(newClickEvent(r, shortID))

	previewData := s.buildPreviewData(payload)
	tmpl := s.templates["preview"]
	if tmpl == nil {
		http.Redirect(w, r, payload.URL, http.StatusFound)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, previewData); err != nil {
		s.config.Logger.Error("template execution failed", "error", err, "shortID", shortID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writePreviewHTML(w, buf.Bytes())
	s.config.Logger.Info("preview rendered (no redirect)", "shortID", shortID, "type", payload.Type, "duration", time.Since(start).String())
}

func (s *Service) handleRedirect(w http.ResponseWriter, r *http.Request) {
	ua := r.UserAgent()
	pathAndQuery := strings.TrimPrefix(r.URL.Query().Get("path_and_query"), "/")
	encoded := url.QueryEscape(pathAndQuery)

	var target string
	switch {
	case strings.Contains(ua, "Android") && s.config.AndroidStoreURL != "":
		target = s.config.AndroidStoreURL + "&referrer=" + encoded
	case (strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad")) && s.config.IOSStoreURL != "":
		target = s.config.IOSStoreURL + "?referrer=" + encoded
	default:
		target = s.config.WebFallbackURL
	}

	if target == "" {
		s.config.Logger.Warn("redirect target missing", "ua", ua)
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, target, http.StatusFound)
}

// handleStats serves aggregated click analytics for a link.
func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	shortID := r.PathValue("shortID")

	payload, err := s.config.Store.Get(r.Context(), shortID)
	if err != nil || payload.DeletedAt != "" {
		s.config.Logger.Warn("stats link not found", "shortID", shortID, "error", err)
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	stats, err := s.config.Store.Stats(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Error("failed to fetch stats", "error", err, "shortID", shortID)
		http.Error(w, "stats error", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// listLinksByType collects every non-deleted link for linkType, prefixes
// the short ID with BaseURL, and stops once the result reaches limit
// (pass 0 for no limit).
func (s *Service) listLinksByType(ctx context.Context, linkType string, limit int) ([]LinkResponse, error) {
	out := make([]LinkResponse, 0)
	var cursor uint64
	for {
		batch, next, err := s.config.Store.List(ctx, linkType, cursor, 100)
		if err != nil {
			return nil, err
		}
		for i := range batch {
			batch[i].ShortLink = s.config.BaseURL + batch[i].ShortLink
			out = append(out, batch[i])
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		if next == 0 {
			return out, nil
		}
		cursor = next
	}
}

func (s *Service) handleLinkList(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	linkType := r.PathValue("type")

	out, err := s.listLinksByType(r.Context(), linkType, 0)
	if err != nil {
		s.config.Logger.Error("failed to list links", "error", err, "type", linkType)
		http.Error(w, "list error", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, out)
	s.config.Logger.Info("links listed", "type", linkType, "count", len(out), "duration", time.Since(start).String())
}

func (s *Service) handleLinkDetail(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	linkType := r.PathValue("type")
	shortID := r.PathValue("shortID")

	payload, err := s.config.Store.Get(r.Context(), shortID)
	if err != nil || payload.DeletedAt != "" {
		s.config.Logger.Warn("link not found", "shortID", shortID, "error", err)
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	if !strings.EqualFold(payload.Type, linkType) {
		s.config.Logger.Warn("link type mismatch", "shortID", shortID, "have", payload.Type, "want", linkType)
		http.Error(w, "link type mismatch", http.StatusBadRequest)
		return
	}

	clicks, _ := s.config.Store.Clicks(r.Context(), shortID)
	respondJSON(w, http.StatusOK, LinkResponse{
		Link:      payload,
		ShortLink: s.config.BaseURL + shortID,
		Clicks:    clicks,
	})
	s.config.Logger.Info("link detail fetched", "shortID", shortID, "duration", time.Since(start).String())
}

// linkListAllCap bounds GET /links so a runaway store cannot OOM the
// dashboard. The dashboard is a single-page view, not a paginated API.
const linkListAllCap = 500

// handleLinkListAll returns every non-deleted link across all registered
// types. The dashboard SPA uses this as its single data source.
func (s *Service) handleLinkListAll(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	out := make([]LinkResponse, 0)
	for _, linkType := range s.registry.Types() {
		remaining := linkListAllCap - len(out)
		if remaining <= 0 {
			break
		}
		batch, err := s.listLinksByType(r.Context(), linkType, remaining)
		if err != nil {
			s.config.Logger.Error("failed to list links", "error", err, "type", linkType)
			http.Error(w, "list error", http.StatusInternalServerError)
			return
		}
		out = append(out, batch...)
	}

	respondJSON(w, http.StatusOK, out)
	s.config.Logger.Info("links listed (all)", "count", len(out), "duration", time.Since(start).String())
}

// linkPatch carries optional updates for PATCH /{shortID}. Pointer fields
// distinguish "omitted" (nil) from "set to zero" (&zero), letting callers
// clear an existing field by sending an explicit empty value.
type linkPatch struct {
	URL         *string        `json:"url,omitempty"`
	Title       *string        `json:"title,omitempty"`
	Description *string        `json:"description,omitempty"`
	ImageURL    *string        `json:"image_url,omitempty"`
	ImageWidth  *int           `json:"image_width,omitempty"`
	ImageHeight *int           `json:"image_height,omitempty"`
	ImageAlt    *string        `json:"image_alt,omitempty"`
	OGType      *string        `json:"og_type,omitempty"`
	Locale      *string        `json:"locale,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// applyTo copies every set field of p onto dst. Type and identity fields
// (ShortID, CreatedAt, Type) are intentionally not patchable.
func (p *linkPatch) applyTo(dst *Link) {
	if p.URL != nil {
		dst.URL = *p.URL
	}
	if p.Title != nil {
		dst.Title = *p.Title
	}
	if p.Description != nil {
		dst.Description = *p.Description
	}
	if p.ImageURL != nil {
		dst.ImageURL = *p.ImageURL
	}
	if p.ImageWidth != nil {
		dst.ImageWidth = *p.ImageWidth
	}
	if p.ImageHeight != nil {
		dst.ImageHeight = *p.ImageHeight
	}
	if p.ImageAlt != nil {
		dst.ImageAlt = *p.ImageAlt
	}
	if p.OGType != nil {
		dst.OGType = *p.OGType
	}
	if p.Locale != nil {
		dst.Locale = *p.Locale
	}
	if p.Metadata != nil {
		dst.Metadata = p.Metadata
	}
}

func (s *Service) handleUpdate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	shortID := r.PathValue("shortID")
	if s.skipPath(shortID) {
		s.config.Logger.Debug("update skipped (path filter)", "shortID", shortID)
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var patch linkPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		s.respondError(w, NewError(err, http.StatusBadRequest, "invalid JSON payload"))
		return
	}

	payload, err := s.config.Store.Get(r.Context(), shortID)
	if err != nil || payload.DeletedAt != "" {
		s.respondError(w, ErrNotFound)
		return
	}

	patch.applyTo(payload)
	payload.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.config.Store.Save(r.Context(), shortID, payload); err != nil {
		s.config.Logger.Error("failed to save updated link", "error", err, "shortID", shortID)
		s.respondError(w, NewError(err, http.StatusInternalServerError, "failed to update link"))
		return
	}

	clicks, _ := s.config.Store.Clicks(r.Context(), shortID)
	respondJSON(w, http.StatusOK, LinkResponse{
		Link:      payload,
		ShortLink: s.config.BaseURL + shortID,
		Clicks:    clicks,
	})
	s.config.Logger.Info("link updated", "shortID", shortID, "duration", time.Since(start).String())
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	shortID := r.PathValue("shortID")
	if s.skipPath(shortID) {
		s.config.Logger.Debug("delete skipped (path filter)", "shortID", shortID)
		http.NotFound(w, r)
		return
	}

	if err := s.config.Store.Delete(r.Context(), shortID); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.respondError(w, ErrNotFound)
			return
		}
		s.config.Logger.Error("failed to delete link", "error", err, "shortID", shortID)
		s.respondError(w, NewError(err, http.StatusInternalServerError, "failed to delete link"))
		return
	}

	w.WriteHeader(http.StatusNoContent)
	s.config.Logger.Info("link deleted", "shortID", shortID, "duration", time.Since(start).String())
}

func (s *Service) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if s.config.TemplateDir == "" {
		s.config.Logger.Debug(".well-known disabled (no TemplateDir)", "path", r.URL.Path)
		http.NotFound(w, r)
		return
	}

	fileName := r.URL.Path[len("/.well-known/"):]
	base := filepath.Join(s.config.TemplateDir, ".well-known")
	filePath := filepath.Join(base, filepath.Clean("/"+fileName))

	if !strings.HasPrefix(filePath, base) {
		s.config.Logger.Warn(".well-known path traversal blocked", "requested", fileName)
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, filePath)
}

// handleTypes returns all registered processor type names for the
// dashboard's create-form dropdown.
func (s *Service) handleTypes(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, s.registry.Types())
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Service) withCORS(h http.HandlerFunc) http.HandlerFunc {
	allowed := make(map[string]bool, len(s.config.AllowedOrigins))
	for _, o := range s.config.AllowedOrigins {
		allowed[o] = true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		h(w, r)
	}
}

func (s *Service) respondError(w http.ResponseWriter, err error) {
	if appErr, ok := errors.AsType[*Error](err); ok {
		// 4xx responses are caller mistakes (bad input, missing record);
		// log at Warn so noisy clients don't drown real Errors.
		if appErr.Code >= 500 {
			s.config.Logger.Error("request error", "error", appErr, "status", appErr.Code)
		} else {
			s.config.Logger.Warn("request error", "error", appErr, "status", appErr.Code)
		}
		http.Error(w, appErr.Message, appErr.Code)
		return
	}
	s.config.Logger.Error("request error", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
