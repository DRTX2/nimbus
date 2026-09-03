package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const developmentAPIKey = "nimbus-local-development-key"

const maxExternalIDsConfigBytes = 64 << 10

var externalIDRefPattern = regexp.MustCompile(`^aws-external-id/[a-z0-9][a-z0-9._-]{0,63}$`)

type Config struct {
	Component                  string
	Environment                string
	HTTPAddress                string
	DatabaseURL                string
	APIKey                     string
	TenantID                   string
	TenantName                 string
	WorkerCount                int
	PollInterval               time.Duration
	LeaseDuration              time.Duration
	MigrationTimeout           time.Duration
	ShutdownTimeout            time.Duration
	OperationalMetricsInterval time.Duration
	OTLPEndpoint               string
	OutboundAllowedHosts       []string
	AWSExternalIDs             map[string]string
}

func Load() (Config, error) {
	component := env("NIMBUS_COMPONENT", "all")
	environment := strings.ToLower(env("NIMBUS_ENV", "development"))
	externalIDs, err := parseExternalIDs(os.Getenv("NIMBUS_AWS_EXTERNAL_IDS"))
	if err != nil {
		return Config{}, err
	}
	apiKey := ""
	if component == "migrate" {
		apiKey = strings.TrimSpace(os.Getenv("NIMBUS_API_KEY"))
		if apiKey == "" && environment == "development" {
			apiKey = developmentAPIKey
		}
	}
	cfg := Config{
		Component:                  component,
		Environment:                environment,
		HTTPAddress:                env("NIMBUS_HTTP_ADDRESS", ":8080"),
		DatabaseURL:                env("NIMBUS_DATABASE_URL", "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"),
		APIKey:                     apiKey,
		TenantID:                   env("NIMBUS_TENANT_ID", "00000000-0000-0000-0000-000000000001"),
		TenantName:                 env("NIMBUS_TENANT_NAME", "Nimbus Local"),
		WorkerCount:                envInt("NIMBUS_WORKER_COUNT", 2),
		PollInterval:               envDuration("NIMBUS_POLL_INTERVAL", 500*time.Millisecond),
		LeaseDuration:              envDuration("NIMBUS_LEASE_DURATION", 45*time.Second),
		MigrationTimeout:           envDuration("NIMBUS_MIGRATION_TIMEOUT", 2*time.Minute),
		ShutdownTimeout:            envDuration("NIMBUS_SHUTDOWN_TIMEOUT", 15*time.Second),
		OperationalMetricsInterval: envDuration("NIMBUS_OPERATIONAL_METRICS_INTERVAL", 15*time.Second),
		OTLPEndpoint:               strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		AWSExternalIDs:             externalIDs,
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
	if cfg.OperationalMetricsInterval < 5*time.Second {
		return Config{}, errors.New("NIMBUS_OPERATIONAL_METRICS_INTERVAL must be at least 5s")
	}
	if cfg.Component == "migrate" {
		if len(cfg.APIKey) < 20 {
			return Config{}, errors.New("NIMBUS_API_KEY must contain at least 20 characters for the migrate component")
		}
		if cfg.Environment != "development" && cfg.APIKey == developmentAPIKey {
			return Config{}, errors.New("NIMBUS_API_KEY must be changed outside development")
		}
	}

	return cfg, nil
}

func parseExternalIDs(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	if len(raw) > maxExternalIDsConfigBytes {
		return nil, errors.New("NIMBUS_AWS_EXTERNAL_IDS exceeds 64 KiB")
	}
	values := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("parse NIMBUS_AWS_EXTERNAL_IDS: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("NIMBUS_AWS_EXTERNAL_IDS must contain one JSON object")
	}
	if len(values) > 100 {
		return nil, errors.New("NIMBUS_AWS_EXTERNAL_IDS must contain at most 100 references")
	}
	for ref, value := range values {
		if !externalIDRefPattern.MatchString(ref) {
			return nil, fmt.Errorf("NIMBUS_AWS_EXTERNAL_IDS contains invalid reference %q", ref)
		}
		if !utf8.ValidString(value) || len(value) < 2 || len(value) > 1224 {
			return nil, fmt.Errorf("NIMBUS_AWS_EXTERNAL_IDS value for %q must contain 2 to 1224 valid UTF-8 bytes", ref)
		}
	}
	return values, nil
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
