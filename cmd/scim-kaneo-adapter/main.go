package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/roberteggl/scim-kaneo-adapter/internal/config"
	"github.com/roberteggl/scim-kaneo-adapter/internal/kaneo"
	"github.com/roberteggl/scim-kaneo-adapter/internal/reconcile"
	"github.com/roberteggl/scim-kaneo-adapter/internal/scim"
	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	st := store.New(cfg.StoreFile)
	client := kaneo.New(cfg.KaneoURL, cfg.KaneoAPIKey)
	engine := &reconcile.Engine{
		Store:       st,
		Assignments: cfg.Assignments,
		Kaneo:       client,
	}
	server := scim.NewServer(st, cfg.SCIMToken, engine)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           accessLog(server.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"addr", cfg.ListenAddr,
			"assignments", len(cfg.Assignments.Assignments),
			"workspaces", cfg.Assignments.Workspaces(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("server", "err", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
