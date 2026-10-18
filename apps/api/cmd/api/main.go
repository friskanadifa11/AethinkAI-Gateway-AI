// Command api runs the AethinkAI Gateway: an OpenAI-compatible routing gateway
// in front of one or more upstream providers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/config"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/gateway"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

func main() {
	// The image uses the binary itself as its health probe, so the runtime has
	// no extra tooling.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// healthcheck performs a bounded GET against the local /healthz endpoint.
func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	addr = strings.Replace(addr, "0.0.0.0", "127.0.0.1", 1)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()

	st, err := store.Open(startupCtx, cfg.DatabaseURL, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(startupCtx, logger); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := st.PurgeExpiredSessions(startupCtx); err != nil {
		// A failed cleanup is not fatal: sign-in checks expiry itself.
		logger.Warn("could not purge expired sessions", "error", err)
	}

	srv := gateway.New(cfg, st, logger)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No write timeout: a streaming completion legitimately runs long, and
		// each upstream attempt is bounded by its own route timeout.
		WriteTimeout: 0,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	go housekeeping(ctx, st, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("gateway listening", "addr", cfg.Addr, "console", cfg.WebDist)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped cleanly")
	return nil
}

// housekeeping applies the retention window on a timer, so an installation that
// runs for months does not grow without bound.
func housekeeping(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings, err := st.LoadSettings(ctx)
			if err != nil {
				logger.Warn("retention: could not read settings", "error", err)
				continue
			}
			days := settings.RetentionDay
			if days <= 0 {
				days = 30
			}
			cutoff := time.Now().AddDate(0, 0, -days)
			if n, err := st.PurgeRequests(ctx, cutoff); err != nil {
				logger.Warn("retention: purge failed", "error", err)
			} else if n > 0 {
				logger.Info("retention: purged old requests", "rows", n, "older_than_days", days)
			}
			if err := st.PurgeExpiredSessions(ctx); err != nil {
				logger.Warn("retention: session purge failed", "error", err)
			}
		}
	}
}

func logLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
