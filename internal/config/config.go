package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const developmentAPIKey = "nimbus-local-development-key"

const maxExternalIDsConfigBytes = 64 << 10

var externalIDRefPattern = regexp.MustCompile(`^aws-external-id/[a-z0-9][a-z0-9._-]{0,63}$`)

var supportedOIDCAlgorithms = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "PS256", "EdDSA"}

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
	OIDC                       OIDC
	WebhookSecrets             []string
	MaxIngestionRate           int
	MaxConcurrentSteps         int
	WebDir                     string
}

// OIDC configures an optional external identity provider for API access.
type OIDC struct {
	Issuer      string
	Audience    string
	Algorithms  []string
	TenantClaim string
}

func (o OIDC) Enabled() bool { return o.Issuer != "" }

// Load reads configuration from the environment and fails closed on any
// malformed value instead of silently falling back to a default.
func Load() (Config, error) {
	var problems []error
	component := env("NIMBUS_COMPONENT", "all")
	environment := strings.ToLower(env("NIMBUS_ENV", "development"))
	externalIDs, err := parseExternalIDs(os.Getenv("NIMBUS_AWS_EXTERNAL_IDS"))
	if err != nil {
		problems = append(problems, err)
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
		WorkerCount:                envInt("NIMBUS_WORKER_COUNT", 2, &problems),
		PollInterval:               envDuration("NIMBUS_POLL_INTERVAL", 500*time.Millisecond, &problems),
		LeaseDuration:              envDuration("NIMBUS_LEASE_DURATION", 45*time.Second, &problems),
		MigrationTimeout:           envDuration("NIMBUS_MIGRATION_TIMEOUT", 2*time.Minute, &problems),
		ShutdownTimeout:            envDuration("NIMBUS_SHUTDOWN_TIMEOUT", 15*time.Second, &problems),
		OperationalMetricsInterval: envDuration("NIMBUS_OPERATIONAL_METRICS_INTERVAL", 15*time.Second, &problems),
		OTLPEndpoint:               strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		AWSExternalIDs:             externalIDs,
		OIDC: OIDC{
			Issuer:      strings.TrimSpace(os.Getenv("NIMBUS_OIDC_ISSUER")),
			Audience:    strings.TrimSpace(os.Getenv("NIMBUS_OIDC_AUDIENCE")),
			Algorithms:  csv(env("NIMBUS_OIDC_ALGORITHMS", "RS256,ES256"), false),
			TenantClaim: strings.TrimSpace(os.Getenv("NIMBUS_OIDC_TENANT_CLAIM")),
		},
		MaxIngestionRate:   envInt("NIMBUS_MAX_INGESTION_RATE", 1000, &problems),
		MaxConcurrentSteps: envInt("NIMBUS_MAX_CONCURRENT_STEPS", 50, &problems),
		WebDir:             strings.TrimSpace(os.Getenv("NIMBUS_WEB_DIR")),
	}
	cfg.OutboundAllowedHosts = csv(os.Getenv("NIMBUS_OUTBOUND_ALLOWED_HOSTS"), true)
	for _, name := range []string{"NIMBUS_WEBHOOK_SECRET", "NIMBUS_WEBHOOK_PREVIOUS_SECRET"} {
		if secret := strings.TrimSpace(os.Getenv(name)); secret != "" {
			if len(secret) < 16 {
				problems = append(problems, fmt.Errorf("%s must contain at least 16 characters", name))
			}
			cfg.WebhookSecrets = append(cfg.WebhookSecrets, secret)
		}
	}

	if cfg.WorkerCount < 1 || cfg.WorkerCount > 64 {
		problems = append(problems, errors.New("NIMBUS_WORKER_COUNT must be between 1 and 64"))
	}
	if !slices.Contains([]string{"all", "api", "worker", "migrate"}, cfg.Component) {
		problems = append(problems, errors.New("NIMBUS_COMPONENT must be all, api, worker, or migrate"))
	}
	if cfg.LeaseDuration <= 30*time.Second {
		problems = append(problems, errors.New("NIMBUS_LEASE_DURATION must exceed the maximum 30s step timeout"))
	}
	if cfg.OperationalMetricsInterval < 5*time.Second {
		problems = append(problems, errors.New("NIMBUS_OPERATIONAL_METRICS_INTERVAL must be at least 5s"))
	}
	if cfg.Component == "migrate" {
		if len(cfg.APIKey) < 20 {
			problems = append(problems, errors.New("NIMBUS_API_KEY must contain at least 20 characters for the migrate component"))
		}
		if cfg.Environment != "development" && cfg.APIKey == developmentAPIKey {
			problems = append(problems, errors.New("NIMBUS_API_KEY must be changed outside development"))
		}
	}
	if cfg.OIDC.Enabled() {
		if cfg.OIDC.Audience == "" {
			problems = append(problems, errors.New("NIMBUS_OIDC_AUDIENCE is required when NIMBUS_OIDC_ISSUER is set"))
		}
		if cfg.Environment != "development" && !strings.HasPrefix(cfg.OIDC.Issuer, "https://") {
			problems = append(problems, errors.New("NIMBUS_OIDC_ISSUER must use https outside development"))
		}
		for _, algorithm := range cfg.OIDC.Algorithms {
			if !slices.Contains(supportedOIDCAlgorithms, algorithm) {
				problems = append(problems, fmt.Errorf("NIMBUS_OIDC_ALGORITHMS contains unsupported algorithm %q", algorithm))
			}
		}
	}
	if cfg.MaxIngestionRate < 1 {
		problems = append(problems, errors.New("NIMBUS_MAX_INGESTION_RATE must be greater than 0"))
	}
	if cfg.MaxConcurrentSteps < 1 {
		problems = append(problems, errors.New("NIMBUS_MAX_CONCURRENT_STEPS must be greater than 0"))
	}
	if cfg.WebDir != "" {
		if info, err := os.Stat(cfg.WebDir); err != nil || !info.IsDir() {
			problems = append(problems, errors.New("NIMBUS_WEB_DIR must reference an existing directory"))
		}
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(problems...))
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

func csv(raw string, lower bool) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int, problems *[]error) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		*problems = append(*problems, fmt.Errorf("%s must be an integer", name))
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration, problems *[]error) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		*problems = append(*problems, fmt.Errorf("%s must be a positive duration such as 500ms or 45s", name))
		return fallback
	}
	return parsed
}
