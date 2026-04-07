package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yinebebt/deeplink"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("deeplink failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger()

	listenAddr := env("DEEPLINK_LISTEN_ADDR", ":8090")
	baseURL := env("DEEPLINK_BASE_URL", "http://localhost:8090/")
	templateDir := discoverTemplateDir(os.Getenv("DEEPLINK_TEMPLATE_DIR"))
	skipPaths, err := loadSkipPaths(os.Getenv("DEEPLINK_SKIP_PATHS_FILE"))
	if err != nil {
		return err
	}

	redisAddr := env("DEEPLINK_REDIS_ADDR", "localhost:6379")
	redisPassword := os.Getenv("DEEPLINK_REDIS_PASSWORD")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Warn("failed to close redis client", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}

	clickBufSize, _ := strconv.Atoi(env("DEEPLINK_CLICK_BUFFER_SIZE", "0"))
	clickFlushInterval, _ := time.ParseDuration(env("DEEPLINK_CLICK_FLUSH_INTERVAL", "0"))

	cfg := deeplink.Config{
		BaseURL:            baseURL,
		Store:              deeplink.NewRedisStore(redisClient),
		Logger:             logger,
		TemplateDir:        templateDir,
		SkipPaths:          skipPaths,
		AllowedOrigins:     splitCommaList(os.Getenv("DEEPLINK_ALLOWED_ORIGINS")),
		ClickBufferSize:    clickBufSize,
		ClickFlushInterval: clickFlushInterval,
		AndroidStoreURL:    os.Getenv("DEEPLINK_ANDROID_STORE_URL"),
		IOSStoreURL:        os.Getenv("DEEPLINK_IOS_STORE_URL"),
		WebFallbackURL:     os.Getenv("DEEPLINK_WEB_FALLBACK_URL"),
	}

	service, err := deeplink.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close() //nolint:errcheck
	service.Register(deeplink.RedirectProcessor{})

	mux := http.NewServeMux()
	mux.Handle("/", service.Handler())

	if dashTmpl := loadDashboardTemplate(templateDir); dashTmpl != nil {
		mux.HandleFunc("GET /dashboard", handleDashboard(service, dashTmpl, logger))
		logger.Info("dashboard enabled at /dashboard")
	}

	var handler http.Handler = mux
	if apiKey := os.Getenv("DEEPLINK_API_KEY"); apiKey != "" {
		handler = withAPIKey(handler, apiKey)
		logger.Info("API key protection enabled for mutating endpoints")
	}

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting deeplink", "listen_addr", listenAddr, "base_url", baseURL, "template_dir", templateDir)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down deeplink")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("DEBUG"), "true") {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCommaList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func discoverTemplateDir(configured string) string {
	if configured != "" {
		return configured
	}
	dir := filepath.Join("templates", "default")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	return ""
}

// withAPIKey protects POST requests with a constant-time token check.
// Accepts both "Authorization: Bearer <key>" and "X-API-Key: <key>".
// GET routes (redirects, previews, dashboard) are not affected.
func withAPIKey(next http.Handler, key string) http.Handler {
	keyBytes := []byte(key)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			token := r.Header.Get("X-API-Key")
			if token == "" {
				token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(token), keyBytes) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// dashboardMaxLinks caps the number of links loaded into memory per
// request. The dashboard is a single-page HTML view, not a paginated
// API — keeping this bounded avoids memory spikes on large instances.
const dashboardMaxLinks = 500

type dashboardLink struct {
	ShortID   string
	ShortLink string
	URL       string
	Clicks    int64
}

type dashboardData struct {
	Links       []dashboardLink
	TotalLinks  int
	TotalClicks int64
}

func loadDashboardTemplate(templateDir string) *template.Template {
	if templateDir == "" {
		return nil
	}
	path := filepath.Join(templateDir, "dashboard.html")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		return nil
	}
	return tmpl
}

func handleDashboard(service *deeplink.Service, tmpl *template.Template, logger *slog.Logger) http.HandlerFunc {
	cfg := service.Config()
	return func(w http.ResponseWriter, r *http.Request) {
		var raw []deeplink.LinkInfo
		for _, linkType := range service.Types() {
			var cursor uint64
			for {
				links, next, err := cfg.Store.List(r.Context(), linkType, cursor, 100)
				if err != nil {
					logger.Error("dashboard: failed to list links", "error", err, "type", linkType)
					break
				}
				raw = append(raw, links...)
				if len(raw) >= dashboardMaxLinks {
					break
				}
				if next == 0 {
					break
				}
				cursor = next
			}
			if len(raw) >= dashboardMaxLinks {
				raw = raw[:dashboardMaxLinks]
				break
			}
		}

		links := make([]dashboardLink, len(raw))
		var totalClicks int64
		for i, l := range raw {
			links[i] = dashboardLink{
				ShortID:   l.ShortLink,
				ShortLink: cfg.BaseURL + l.ShortLink,
				URL:       l.URL,
				Clicks:    l.Clicks,
			}
			totalClicks += l.Clicks
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, dashboardData{
			Links:       links,
			TotalLinks:  len(links),
			TotalClicks: totalClicks,
		}); err != nil {
			logger.Error("dashboard: template error", "error", err)
			http.Error(w, "render error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write(buf.Bytes())
	}
}

func loadSkipPaths(configured string) ([]string, error) {
	path := configured
	if path == "" {
		path = "skip_path.json"
		if _, err := os.Stat(path); err != nil {
			return nil, nil
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open skip paths file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	var data struct {
		Path []string `json:"path"`
	}
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode skip paths file: %w", err)
	}
	return data.Path, nil
}
