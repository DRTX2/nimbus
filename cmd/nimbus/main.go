package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/david/nimbus/internal/awsmonitor"
	"github.com/david/nimbus/internal/config"
	"github.com/david/nimbus/internal/execution"
	"github.com/david/nimbus/internal/httpapi"
	"github.com/david/nimbus/internal/platform/database"
	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("Nimbus stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repository := store.New(pool)
	if cfg.Component == "migrate" {
		migrationCtx, cancel := context.WithTimeout(rootCtx, cfg.MigrationTimeout)
		defer cancel()
		if err := database.Migrate(migrationCtx, pool); err != nil {
			return err
		}
		if err := repository.BootstrapTenant(migrationCtx, cfg.TenantID, cfg.TenantName); err != nil {
			return err
		}
		if err := repository.BootstrapOwner(migrationCtx, cfg.TenantID, "Nimbus Local Owner", cfg.APIKey); err != nil {
			return err
		}
		logger.Info("Nimbus migrations and bootstrap completed")
		return nil
	}
	if err := database.VerifySchema(rootCtx, pool); err != nil {
		return err
	}

	shutdownTracing, err := observability.SetupTracing(rootCtx, cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			logger.Error("trace shutdown failed", "error", err)
		}
	}()

	runsWorkers := cfg.Component == "all" || cfg.Component == "worker"
	metrics := observability.NewMetrics(runsWorkers)
	var workers sync.WaitGroup
	if runsWorkers {
		worker := execution.NewWorker(repository, logger, metrics, cfg.WorkerCount, cfg.PollInterval, cfg.LeaseDuration, cfg.OutboundAllowedHosts)
		publisher := execution.NewOutboxPublisher(repository, logger, cfg.PollInterval, cfg.LeaseDuration)
		operationalMetrics := observability.NewOperationalRefresher(repository, metrics, logger, cfg.OperationalMetricsInterval)
		workers.Add(3)
		go func() { defer workers.Done(); worker.Run(rootCtx) }()
		go func() { defer workers.Done(); publisher.Run(rootCtx) }()
		go func() { defer workers.Done(); operationalMetrics.Run(rootCtx) }()
	}
	handler := httpapi.NewOperational(repository, metrics, logger)
	if cfg.Component == "all" || cfg.Component == "api" {
		var oidcValidator *httpapi.OIDCValidator
		if cfg.OIDCIssuer != "" {
			oidcValidator, err = httpapi.NewOIDCValidator(cfg.OIDCIssuer, cfg.OIDCAudience)
			if err != nil {
				return err
			}
		}
		handler = httpapi.New(repository, metrics, logger, awsmonitor.NewCollector(cfg.AWSExternalIDs), oidcValidator, cfg.OIDCIssuer, cfg.TenantID)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Nimbus started", "address", cfg.HTTPAddress, "environment", cfg.Environment, "component", cfg.Component, "workers", cfg.WorkerCount)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		workers.Wait()
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
