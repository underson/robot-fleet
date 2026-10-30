// Command fleetd is the fleet telemetry API server. It only wires things
// together: configuration, storage, use cases and HTTP.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/underson/robot-fleet/backend/internal/adapters/httpapi"
	"github.com/underson/robot-fleet/backend/internal/adapters/postgres"
	"github.com/underson/robot-fleet/backend/internal/config"
	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fleetd stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}
	store := postgres.NewStore(pool)
	if cfg.SeedDemo {
		if err := store.SeedDemo(ctx); err != nil {
			return err
		}
		logger.Info("demo data seeded")
	}

	ingestor := telemetry.NewIngestor(store, store, store, time.Now, cfg.MaxClockSkew)
	fleetService := fleet.NewService(store, cfg.HealthPolicy(), time.Now, cfg.HistoryWindow)
	api := httpapi.NewServer(ingestor, fleetService, pool, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	logger.Info("shutting down")
	return srv.Shutdown(shutdownCtx)
}
