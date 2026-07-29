package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const developmentAPIKey = "nimbus-local-development-key"

type Config struct {
	Component            string
	Environment          string
	HTTPAddress          string
	DatabaseURL          string
	APIKey               string
	TenantID             string
	TenantName           string
	WorkerCount          int
	PollInterval         time.Duration
	LeaseDuration        time.Duration
	MigrationTimeout     time.Duration
	ShutdownTimeout      time.Duration
	OTLPEndpoint         string
	OutboundAllowedHosts []string
}

func Load() (Config, error) {
	cfg := Config{
		Component:        env("NIMBUS_COMPONENT", "all"),
		Environment:      env("NIMBUS_ENV", "development"),
		HTTPAddress:      env("NIMBUS_HTTP_ADDRESS", ":8080"),
		DatabaseURL:      env("NIMBUS_DATABASE_URL", "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"),
		APIKey:           env("NIMBUS_API_KEY", developmentAPIKey),
		TenantID:         env("NIMBUS_TENANT_ID", "00000000-0000-0000-0000-000000000001"),
		TenantName:       env("NIMBUS_TENANT_NAME", "Nimbus Local"),
		WorkerCount:      envInt("NIMBUS_WORKER_COUNT", 2),
		PollInterval:     envDuration("NIMBUS_POLL_INTERVAL", 500*time.Millisecond),
		LeaseDuration:    envDuration("NIMBUS_LEASE_DURATION", 45*time.Second),
		MigrationTimeout: envDuration("NIMBUS_MIGRATION_TIMEOUT", 2*time.Minute),
		ShutdownTimeout:  envDuration("NIMBUS_SHUTDOWN_TIMEOUT", 15*time.Second),
		OTLPEndpoint:     strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
	}

	for _, host := range strings.Split(os.Getenv("NIMBUS_OUTBOUND_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(strings.ToLower(host)); host != "" {
			cfg.OutboundAllowedHosts = append(cfg.OutboundAllowedHosts, host)
		}
	}

	if cfg.WorkerCount < 1 || cfg.WorkerCount > 64 {
		return Config{}, fmt.Errorf("NIMBUS_WORKER_COUNT must be between 1 and 64")
	}
	if cfg.Component != "all" && cfg.Component != "api" && cfg.Component != "worker" && cfg.Component != "migrate" {
		return Config{}, errors.New("NIMBUS_COMPONENT must be all, api, worker, or migrate")
	}
	if cfg.LeaseDuration <= 30*time.Second {
		return Config{}, errors.New("NIMBUS_LEASE_DURATION must exceed the maximum 30s step timeout")
	}
	if len(cfg.APIKey) < 20 {
		return Config{}, errors.New("NIMBUS_API_KEY must contain at least 20 characters")
	}
	if cfg.Environment == "production" && cfg.APIKey == developmentAPIKey {
		return Config{}, errors.New("NIMBUS_API_KEY must be changed in production")
	}

	return cfg, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
