package deeplink

import (
	"bytes"
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
//	POST /shorten          create a short link
//	GET  /{shortID}              preview page (or 302 if no template)
//	GET  /links/{type}           list links by type and environment
//	GET  /links/{type}/{shortID} single link with click count
//	GET  /health                 health check
//
// When store URL fields are set in [Config], these are also registered:
//
//	GET  /redirect          platform-aware app store redirect
//	GET  /preview/{shortID}      preview without auto-redirect
//	GET  /.well-known/           static files from TemplateDir
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /shorten", s.handleGenerate)
	mux.HandleFunc("GET /links/{type}", s.handleLinkList)
	mux.HandleFunc("GET /links/{type}/{shortID}", s.handleLinkDetail)
	mux.HandleFunc("GET /health", handleHealth)

	if s.config.hasMobileRoutes() {
		mux.HandleFunc("GET /.well-known/", s.handleWellKnown)
		mux.HandleFunc("GET /redirect", s.handleRedirect)
		mux.HandleFunc("GET /preview/{shortID}", s.handleStaticPreview)
	}

	mux.HandleFunc("GET /{shortID}", s.handlePreview)

	return s.withCORS(mux.ServeHTTP)
}

func (s *Service) handleGenerate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	env := s.envFromRequest(r)

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	var payload Link
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.respondError(w, NewError(err, http.StatusBadRequest, "invalid JSON payload"))
		return
	}

	payload.Environment = env

	processor := s.registry.Get(strings.ToLower(payload.Type))
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
		s.config.Logger.Error("failed to shorten URL", "error", err, "type", payload.Type, "env", env)
		s.respondError(w, NewError(err, http.StatusInternalServerError, "failed to shorten URL"))
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"short_url": shortURL})
	s.config.Logger.Info("link generated", "shortID", strings.TrimPrefix(shortURL, s.config.BaseURL), "type", payload.Type, "env", env, "duration", time.Since(start))
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	env := s.envFromRequest(r)

	shortID := r.PathValue("shortID")
	if s.skipPath(shortID) {
		http.NotFound(w, r)
		return
	}

	payload, err := s.expandURL(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Error("failed to expand URL", "error", err, "shortID", shortID, "env", env)
		http.NotFound(w, r)
		return
	}

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

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
	s.config.Logger.Info("preview rendered", "shortID", shortID, "type", payload.Type, "env", env, "duration", time.Since(start))
}

// handleStaticPreview renders a preview page without auto-redirect.
// Useful as an intermediate page for iOS Universal Links or any
// flow where the user should tap to continue.
func (s *Service) handleStaticPreview(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	env := s.envFromRequest(r)
	w.Header().Set("Content-Type", "text/html")

	shortID := r.PathValue("shortID")

	payload, err := s.expandURL(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Error("failed to expand URL", "error", err, "shortID", shortID, "env", env)
		http.NotFound(w, r)
		return
	}

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

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
	s.config.Logger.Info("preview rendered (no redirect)", "shortID", shortID, "type", payload.Type, "env", env, "duration", time.Since(start))
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
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Service) handleLinkList(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	env := s.envFromRequest(r)
	linkType := r.PathValue("type")

	var allLinks []LinkInfo
	var cursor uint64
	for {
		links, next, err := s.config.Store.List(r.Context(), linkType, env, cursor, 100)
		if err != nil {
			s.config.Logger.Error("failed to list links", "error", err, "type", linkType, "env", env)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		allLinks = append(allLinks, links...)
		if next == 0 {
			break
		}
		cursor = next
	}

	if allLinks == nil {
		allLinks = []LinkInfo{}
	}

	for i := range allLinks {
		allLinks[i].ShortLink = s.config.BaseURL + allLinks[i].ShortLink
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(allLinks); err != nil {
		s.config.Logger.Error("failed to encode response", "error", err)
	}

	s.config.Logger.Info("links listed", "type", linkType, "count", len(allLinks), "env", env, "duration", time.Since(start))
}

func (s *Service) handleLinkDetail(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	env := s.envFromRequest(r)
	linkType := r.PathValue("type")
	shortID := r.PathValue("shortID")

	payload, err := s.config.Store.Get(r.Context(), shortID)
	if err != nil {
		s.config.Logger.Warn("link not found", "shortId", shortID, "env", env)
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	if !strings.EqualFold(payload.Type, linkType) {
		http.Error(w, "link type mismatch", http.StatusBadRequest)
		return
	}

	clicks, _ := s.config.Store.Clicks(r.Context(), shortID)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(LinkInfo{ShortLink: s.config.BaseURL + shortID, URL: payload.URL, Clicks: clicks}); err != nil {
		s.config.Logger.Error("failed to encode response", "error", err)
	}

	s.config.Logger.Info("link detail fetched", "shortId", shortID, "env", env, "duration", time.Since(start))
}

func (s *Service) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if s.config.TemplateDir == "" {
		http.NotFound(w, r)
		return
	}

	fileName := r.URL.Path[len("/.well-known/"):]
	base := filepath.Join(s.config.TemplateDir, ".well-known")
	filePath := filepath.Join(base, filepath.Clean("/"+fileName))

	if !strings.HasPrefix(filePath, base) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, filePath)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Service) buildPreviewData(payload *Link) any {
	processor := s.registry.Get(payload.Type)
	if p, ok := processor.(Previewer); ok {
		if data := p.Preview(payload); data != nil {
			return data
		}
	}
	return payload
}

func (s *Service) envFromRequest(r *http.Request) string {
	if env := r.Header.Get("X-Environment"); env != "" {
		return env
	}
	if env := r.URL.Query().Get("environment"); env != "" {
		return env
	}
	return s.config.DefaultEnvironment
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		h(w, r)
	}
}

func (s *Service) respondError(w http.ResponseWriter, err error) {
	var appErr *Error
	if errors.As(err, &appErr) {
		s.config.Logger.Error("request error", "error", appErr, "status", appErr.Code)
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
