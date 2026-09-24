package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		spec, err := httpapi.OpenAPISpec()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(spec))
		return
	}
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
		return migrate(rootCtx, logger, cfg, repository, pool)
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
	var background sync.WaitGroup
	if runsWorkers {
		startWorkers(rootCtx, &background, logger, cfg, repository, metrics)
	}
	handler := httpapi.NewOperational(repository, metrics, logger)
	if cfg.Component == "all" || cfg.Component == "api" {
		handler, err = apiHandler(rootCtx, logger, cfg, repository, metrics)
		if err != nil {
			return err
		}
	}
	return serve(rootCtx, logger, cfg, handler, &background)
}

func migrate(ctx context.Context, logger *slog.Logger, cfg config.Config, repository *store.Store, pool *pgxpool.Pool) error {
	migrationCtx, cancel := context.WithTimeout(ctx, cfg.MigrationTimeout)
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

func startWorkers(ctx context.Context, group *sync.WaitGroup, logger *slog.Logger, cfg config.Config, repository *store.Store, metrics *observability.Metrics) {
	connectors := execution.DefaultConnectors(cfg.OutboundAllowedHosts, execution.NewSigner(cfg.WebhookSecrets...))
	worker := execution.NewWorker(repository, logger, metrics, connectors, execution.WorkerConfig{
		Count: cfg.WorkerCount, PollInterval: cfg.PollInterval,
		LeaseDuration: cfg.LeaseDuration, MaxConcurrentSteps: cfg.MaxConcurrentSteps,
	})
	publisher := execution.NewOutboxPublisher(repository, logger, cfg.PollInterval, cfg.LeaseDuration)
	operational := observability.NewOperationalRefresher(repository, metrics, logger, cfg.OperationalMetricsInterval)
	maintenance := execution.NewMaintenance(repository, logger, 5*time.Minute)
	for _, runner := range []func(context.Context){worker.Run, publisher.Run, operational.Run, maintenance.Run} {
		group.Go(func() { runner(ctx) })
	}
}

func apiHandler(ctx context.Context, logger *slog.Logger, cfg config.Config, repository *store.Store, metrics *observability.Metrics) (http.Handler, error) {
	var verifier *httpapi.OIDCVerifier
	if cfg.OIDC.Enabled() {
		var err error
		verifier, err = httpapi.NewOIDCVerifier(ctx, httpapi.OIDCConfig{
			Issuer: cfg.OIDC.Issuer, Audience: cfg.OIDC.Audience, Algorithms: cfg.OIDC.Algorithms,
			TenantClaim: cfg.OIDC.TenantClaim, DefaultTenant: cfg.TenantID,
		})
		if err != nil {
			return nil, err
		}
	}
	return httpapi.New(httpapi.Options{
		Store: repository, Metrics: metrics, Logger: logger,
		AWSCollector: awsmonitor.NewCollector(cfg.AWSExternalIDs), OIDC: verifier,
		MaxIngestionRate: cfg.MaxIngestionRate, WebDir: cfg.WebDir,
	}), nil
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, handler http.Handler, background *sync.WaitGroup) error {
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
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		background.Wait()
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}
