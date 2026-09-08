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

	"github.com/dangyyyy/avito-kitchen/internal/pkg/config"
	"github.com/dangyyyy/avito-kitchen/internal/pkg/logger"
	"github.com/dangyyyy/avito-kitchen/internal/restaurant"
	"github.com/dangyyyy/avito-kitchen/internal/restaurant/partnerclient"
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

	client, err := partnerclient.NewClientWithResponses(
		cfg.PlatformURL,
		partnerclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("X-Api-Key", cfg.APIKey)

			return nil
		}),
	)
	if err != nil {
		return err
	}

	kitchen := restaurant.NewKitchen(client, cfg.PollInterval)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           restaurant.NewControlRouter(kitchen),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)

	go func() {
		slog.Info("restaurant control api listening", "addr", cfg.HTTPAddr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	kitchenErr := make(chan error, 1)

	go func() {
		kitchenErr <- kitchen.Run(ctx)
	}()

	select {
	case err := <-serverErr:
		return err
	case err := <-kitchenErr:
		if err != nil {
			return err
		}
	case <-ctx.Done():
	}

	slog.Info("restaurant service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}
