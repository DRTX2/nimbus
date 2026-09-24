package config

import "testing"

func TestParseExternalIDs(t *testing.T) {
	values, err := parseExternalIDs(`{"aws-external-id/acme-prod":"customer-generated-value"}`)
	if err != nil {
		t.Fatalf("parseExternalIDs() error = %v", err)
	}
	if values["aws-external-id/acme-prod"] != "customer-generated-value" {
		t.Fatalf("unexpected values: %#v", values)
	}

	for _, raw := range []string{
		`{"NIMBUS_API_KEY":"not-allowed"}`,
		`{"aws-external-id/acme":"x"}`,
		`[]`,
		`{"aws-external-id/acme":"valid"} {}`,
	} {
		if _, err := parseExternalIDs(raw); err == nil {
			t.Fatalf("parseExternalIDs(%q) accepted invalid configuration", raw)
		}
	}
}

func TestLoadRequiresBootstrapKeyOnlyForMigrator(t *testing.T) {
	t.Setenv("NIMBUS_COMPONENT", "api")
	t.Setenv("NIMBUS_ENV", "production")
	t.Setenv("NIMBUS_API_KEY", "short-runtime-value")
	t.Setenv("NIMBUS_AWS_EXTERNAL_IDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() for API error = %v", err)
	}
	if cfg.APIKey != "" {
		t.Fatal("API component retained the bootstrap owner key")
	}

	t.Setenv("NIMBUS_COMPONENT", "migrate")
	t.Setenv("NIMBUS_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a production migrator without a bootstrap owner key")
	}
}

// Misconfiguration must stop the process rather than run with surprising defaults.
func TestLoadRejectsMalformedValues(t *testing.T) {
	for name, value := range map[string]string{
		"NIMBUS_WORKER_COUNT":         "many",
		"NIMBUS_POLL_INTERVAL":        "-1s",
		"NIMBUS_OIDC_ALGORITHMS":      "none",
		"NIMBUS_WEBHOOK_SECRET":       "short",
		"NIMBUS_WEB_DIR":              "/definitely/not/here",
		"NIMBUS_MAX_CONCURRENT_STEPS": "0",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NIMBUS_COMPONENT", "api")
			t.Setenv("NIMBUS_OIDC_ISSUER", "http://localhost:9999")
			t.Setenv("NIMBUS_OIDC_AUDIENCE", "nimbus")
			t.Setenv(name, value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted %s=%q", name, value)
			}
		})
	}
}
