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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dangyyyy/avito-kitchen/internal/pkg/config"
	"github.com/dangyyyy/avito-kitchen/internal/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		slog.Error("restaurant service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadRestaurant()
	if err != nil {
		return err
	}

	logger.Setup(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)

	go func() {
		slog.Info("restaurant service listening",
			"addr", cfg.HTTPAddr,
			"platform", cfg.PlatformURL,
			"poll_interval", cfg.PollInterval,
		)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	slog.Info("restaurant service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

func newRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return r
}
