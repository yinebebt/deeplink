package main

import (
	"context"
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

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      service.Handler(),
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
