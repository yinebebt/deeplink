package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/redis/go-redis/v9"
	"github.com/yinebebt/deeplink"
	"github.com/yinebebt/deeplink/mcpserver"
	"github.com/yinebebt/deeplink/redisstore"
	"github.com/yinebebt/deeplink/sqlitestore"
	"github.com/yinebebt/keyring"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "keys" {
		if err := runKeys(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, keyStore, closeStore, err := buildStore(ctx, logger)
	if err != nil {
		return err
	}
	defer closeStore()

	clickBufSize, _ := strconv.Atoi(env("DEEPLINK_CLICK_BUFFER_SIZE", "0"))
	clickFlushInterval, _ := time.ParseDuration(env("DEEPLINK_CLICK_FLUSH_INTERVAL", "0"))

	cfg := deeplink.Config{
		BaseURL:            baseURL,
		Store:              store,
		Logger:             logger,
		TemplateDir:        templateDir,
		SkipPaths:          skipPaths,
		AllowedOrigins:     splitCommaList(os.Getenv("DEEPLINK_ALLOWED_ORIGINS")),
		ClickBufferSize:    clickBufSize,
		ClickFlushInterval: clickFlushInterval,
		AndroidStoreURL:    os.Getenv("DEEPLINK_ANDROID_STORE_URL"),
		IOSStoreURL:        os.Getenv("DEEPLINK_IOS_STORE_URL"),
		WebFallbackURL:     os.Getenv("DEEPLINK_WEB_FALLBACK_URL"),
		SiteName:           os.Getenv("DEEPLINK_SITE_NAME"),
		Locale:             os.Getenv("DEEPLINK_LOCALE"),
		TwitterSite:        os.Getenv("DEEPLINK_TWITTER_SITE"),
		FediverseCreator:   os.Getenv("DEEPLINK_FEDIVERSE_CREATOR"),
	}

	service, err := deeplink.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close() //nolint:errcheck
	service.Register(deeplink.RedirectProcessor{})

	if err := seedAPIKey(ctx, keyStore, os.Getenv("DEEPLINK_API_KEY"), logger); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", service.Handler())
	// The SDK's DNS-rebinding check blocks requests from a reverse proxy;
	// /mcp always needs the API key, which covers that risk.
	mux.Handle("/mcp", mcpserver.Handler(service, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
	}))

	handler := withAPIKey(mux, keyStore, logger)

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

// buildStore constructs the store named by DEEPLINK_STORE (default "sqlite"),
// returning it, the API key store on the same backend, and a cleanup func.
// SQLite is persistent and zero-infra; redis suits multi-instance setups;
// memory is non-persistent (dev/tests).
func buildStore(ctx context.Context, logger *slog.Logger) (deeplink.Store, keyring.Store, func(), error) {
	switch backend := env("DEEPLINK_STORE", "sqlite"); backend {
	case "sqlite":
		dsn := env("DEEPLINK_SQLITE_DSN", "deeplink.db")
		// Open the DB here so the API key table can share it.
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open sqlite: %w", err)
		}
		db.SetMaxOpenConns(1) // as in sqlitestore.New
		store, err := sqlitestore.NewWithDB(ctx, db)
		if err != nil {
			_ = db.Close()
			return nil, nil, nil, fmt.Errorf("open sqlite store: %w", err)
		}
		keyStore, err := sqlKeyStore(ctx, db)
		if err != nil {
			_ = db.Close()
			return nil, nil, nil, err
		}
		logger.Info("using sqlite store", "dsn", dsn)
		return store, keyStore, func() { _ = db.Close() }, nil

	case "redis":
		client := redis.NewClient(&redis.Options{
			Addr:     env("DEEPLINK_REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("DEEPLINK_REDIS_PASSWORD"),
			DB:       0,
		})
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := client.Ping(pingCtx).Err(); err != nil {
			_ = client.Close()
			return nil, nil, nil, fmt.Errorf("ping redis: %w", err)
		}
		logger.Info("using redis store")
		return redisstore.New(client), redisKeyStore(client), func() {
			if err := client.Close(); err != nil {
				logger.Warn("failed to close redis client", "error", err)
			}
		}, nil

	case "memory":
		logger.Warn("using in-memory store: links do not survive restart")
		return deeplink.NewMemoryStore(), memKeyStore(), func() {}, nil

	default:
		return nil, nil, nil, fmt.Errorf("unknown DEEPLINK_STORE %q (want sqlite, redis, or memory)", backend)
	}
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

// withAPIKey puts keyring's key check on mutating requests (POST, PATCH,
// DELETE), the dashboard list/stats GETs, and every /mcp request. Preview and
// redirect GETs stay public.
func withAPIKey(next http.Handler, keys keyring.Store, logger *slog.Logger) http.Handler {
	protected := keyring.Middleware(
		keyring.New(keys, 0), // grace only matters to Rotate
		keyring.OnError(func(_ *http.Request, err error) { logger.Error("read API keys", "error", err) }),
	)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requiresAPIKey(r.Method, r.URL.Path) {
			protected.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requiresAPIKey(method, path string) bool {
	if path == "/mcp" {
		return true
	}
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		return true
	case http.MethodGet:
		p := path
		if p != "/" {
			p = strings.TrimSuffix(p, "/")
		}
		if p == "/links" || strings.HasPrefix(p, "/links/") {
			return true
		}
		if p == "/stats" || strings.HasPrefix(p, "/stats/") {
			return true
		}
	}
	return false
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
