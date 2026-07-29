//go:build integration

package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/david/nimbus/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHostedRuntimeRoleBoundary(t *testing.T) {
	ownerURL := requiredEnv(t, "NIMBUS_TEST_DATABASE_URL")
	apiURL := requiredEnv(t, "NIMBUS_TEST_API_DATABASE_URL")
	workerURL := requiredEnv(t, "NIMBUS_TEST_WORKER_DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := openTestPool(t, ctx, ownerURL)
	defer owner.Close()
	if err := Migrate(ctx, owner); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tenantA := randomUUID(t, ctx, owner)
	tenantB := randomUUID(t, ctx, owner)
	token := fmt.Sprintf("nimbus-integration-key-%d", time.Now().UnixNano())
	ownerStore := store.New(owner)
	if err := ownerStore.BootstrapTenant(ctx, tenantA, "Tenant A"); err != nil {
		t.Fatalf("bootstrap tenant A: %v", err)
	}
	if err := ownerStore.BootstrapTenant(ctx, tenantB, "Tenant B"); err != nil {
		t.Fatalf("bootstrap tenant B: %v", err)
	}
	if err := ownerStore.BootstrapOwner(ctx, tenantA, "Tenant A Owner", token); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}

	api := openTestPool(t, ctx, apiURL)
	defer api.Close()
	worker := openTestPool(t, ctx, workerURL)
	defer worker.Close()
	if err := VerifySchema(ctx, api); err != nil {
		t.Fatalf("API verify schema: %v", err)
	}
	if err := VerifySchema(ctx, worker); err != nil {
		t.Fatalf("worker verify schema: %v", err)
	}

	var count int
	if err := api.QueryRow(ctx, "SELECT count(*) FROM workflows").Scan(&count); err != nil {
		t.Fatalf("query without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("missing tenant context exposed %d workflows", count)
	}

	apiStore := store.New(api)
	principal, capability, err := apiStore.AuthenticateAPIKey(ctx, token)
	if err != nil {
		t.Fatalf("authenticate API key: %v", err)
	}
	if principal.TenantID != tenantA {
		t.Fatalf("authenticated tenant = %s, want %s", principal.TenantID, tenantA)
	}
	apiCtx := store.WithTenantCapability(ctx, capability)
	secondaryToken := fmt.Sprintf("nimbus-secondary-key-%d", time.Now().UnixNano())
	secondaryKey, err := apiStore.CreateAPIKey(apiCtx, principal, "Secondary", "viewer", secondaryToken, nil)
	if err != nil {
		t.Fatalf("create API key through scoped principal function: %v", err)
	}
	if _, _, err := apiStore.AuthenticateAPIKey(ctx, secondaryToken); err != nil {
		t.Fatalf("authenticate created API key: %v", err)
	}
	if err := apiStore.RevokeAPIKey(apiCtx, principal, secondaryKey.ID); err != nil {
		t.Fatalf("revoke API key: %v", err)
	}
	if _, _, err := apiStore.AuthenticateAPIKey(ctx, secondaryToken); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("revoked API key authentication error = %v, want forbidden", err)
	}

	workflowA := insertWorkflow(t, ctx, owner, tenantA, tenantA)
	workflowB := insertWorkflow(t, ctx, owner, tenantB, tenantB)
	assertAPIKeyBoundRLS(t, ctx, api, token, tenantA, tenantB, workflowA, workflowB)
	items, err := apiStore.ListWorkflows(apiCtx, tenantA)
	if err != nil {
		t.Fatalf("list with current key capability: %v", err)
	}
	if len(items) != 1 || items[0].ID != workflowA {
		t.Fatal("current key capability did not establish API tenant scope")
	}
	items, err = apiStore.ListWorkflows(apiCtx, tenantB)
	if err != nil {
		t.Fatalf("list with mismatched explicit tenant: %v", err)
	}
	if len(items) != 0 {
		t.Fatal("API capability bypassed the explicit tenant predicate")
	}
	assertRuntimeRestrictions(t, ctx, api, worker, tenantA)

	seedClaimableStep(t, ctx, owner, tenantA, workflowA)
	workerStore := store.New(worker)
	var job store.Job
	var found bool
	for range 20 {
		job, found, err = workerStore.ClaimStep(ctx, "integration-worker", time.Minute)
		if err != nil || found {
			break
		}
	}
	if err != nil {
		t.Fatalf("worker claim: %v", err)
	}
	if !found || job.TenantID != tenantA {
		t.Fatalf("worker claim found=%v tenant=%q, want tenant %q", found, job.TenantID, tenantA)
	}
	assertWorkerTenantLimit(t, ctx, owner, worker)
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func openTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return pool
}

func randomUUID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	return id
}

func insertWorkflow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contextTenant, rowTenant string) string {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workflow insert: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", contextTenant); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	var id string
	if err := tx.QueryRow(ctx, "INSERT INTO workflows (tenant_id, name) VALUES ($1, 'isolated') RETURNING id", rowTenant).Scan(&id); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_versions (tenant_id,workflow_id,version,definition)
		VALUES ($1,$2,1,'{"eventType":"test","steps":[]}')`, rowTenant, id); err != nil {
		t.Fatalf("insert workflow version: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit workflow insert: %v", err)
	}
	return id
}

func assertAPIKeyBoundRLS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token, tenantA, tenantB, workflowA, workflowB string) {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	keyHash := hex.EncodeToString(hash[:])
	tests := []struct {
		name       string
		tenant     string
		keyHash    string
		workflowID string
		want       int
	}{
		{name: "current key sees its tenant", tenant: tenantB, keyHash: keyHash, workflowID: workflowA, want: 1},
		{name: "victim GUC cannot select victim", tenant: tenantB, keyHash: keyHash, workflowID: workflowB, want: 0},
		{name: "missing key sees nothing", tenant: tenantA, workflowID: workflowA, want: 0},
		{name: "incorrect key sees nothing", tenant: tenantA, keyHash: hex.EncodeToString(make([]byte, sha256.Size)), workflowID: workflowA, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", test.tenant)
			if test.keyHash != "" {
				_, _ = tx.Exec(ctx, "SELECT set_config('app.api_key_hash', $1, true)", test.keyHash)
			}
			var count int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM workflows WHERE id=$1", test.workflowID).Scan(&count); err != nil {
				t.Fatalf("query workflow: %v", err)
			}
			if count != test.want {
				t.Fatalf("visible workflows = %d, want %d", count, test.want)
			}
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantB)
	_, _ = tx.Exec(ctx, "SELECT set_config('app.api_key_hash', $1, true)", keyHash)
	if _, err := tx.Exec(ctx, "INSERT INTO workflows (tenant_id,name) VALUES ($1,'forbidden')", tenantB); err == nil {
		t.Fatal("victim tenant GUC allowed a cross-tenant API write")
	}
}

func assertRuntimeRestrictions(t *testing.T, ctx context.Context, api, worker *pgxpool.Pool, tenantID string) {
	t.Helper()
	if _, err := api.Exec(ctx, "CREATE TABLE runtime_must_not_create (id integer)"); err == nil {
		t.Fatal("API role performed DDL")
	}
	if _, err := api.Exec(ctx, "SELECT * FROM schema_migrations"); err == nil {
		t.Fatal("API role read schema_migrations")
	}
	if _, err := api.Exec(ctx, "SELECT * FROM nimbus_worker_tenants()"); err == nil {
		t.Fatal("API role enumerated worker tenants")
	}
	if _, err := worker.Exec(ctx, "SELECT * FROM api_keys"); err == nil {
		t.Fatal("worker role read API keys")
	}
	if _, err := api.Exec(ctx, "SELECT * FROM api_key_scopes"); err == nil {
		t.Fatal("API role read owner-only API key scopes")
	}
	if _, err := api.Exec(ctx, "SELECT * FROM principals"); err == nil {
		t.Fatal("API role read global principals")
	}
	if _, err := worker.Exec(ctx, "SELECT * FROM principals"); err == nil {
		t.Fatal("worker role read global principals")
	}
	if _, err := worker.Exec(ctx, "SELECT * FROM nimbus_authenticate_api_key('x'::bytea)"); err == nil {
		t.Fatal("worker role invoked API authentication")
	}
	tx, err := worker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID)
	var systemPrincipals int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM nimbus_system_principal($1)", tenantID).Scan(&systemPrincipals); err != nil {
		t.Fatalf("load worker system principal: %v", err)
	}
	if systemPrincipals != 1 {
		t.Fatalf("worker system principals = %d, want 1", systemPrincipals)
	}

	if _, err := api.Exec(ctx, "UPDATE audit_events SET action='changed'"); err == nil {
		t.Fatal("API role mutated an audit event")
	}
	if _, err := worker.Exec(ctx, "UPDATE audit_events SET action='changed'"); err == nil {
		t.Fatal("worker role mutated audit events")
	}
	for _, table := range []string{"memberships", "step_runs", "outbox_messages"} {
		if _, err := api.Exec(ctx, "UPDATE "+table+" SET tenant_id=tenant_id"); err == nil {
			t.Fatalf("API role updated %s", table)
		}
	}
}

func assertWorkerTenantLimit(t *testing.T, ctx context.Context, owner, worker *pgxpool.Pool) {
	t.Helper()
	if _, err := owner.Exec(ctx, `INSERT INTO tenants (id,name)
		SELECT md5('tenant-limit-' || value::text)::uuid, 'limit tenant'
		FROM generate_series(1,1005) value ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed tenant limit rows: %v", err)
	}
	tests := []struct {
		name  string
		limit any
		want  int
	}{
		{name: "null defaults", limit: nil, want: 100},
		{name: "zero has minimum", limit: 0, want: 1},
		{name: "oversize is capped", limit: 100000, want: 1000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var count int
			if err := worker.QueryRow(ctx, "SELECT count(*) FROM nimbus_worker_tenants(NULL, $1)", test.limit).Scan(&count); err != nil {
				t.Fatalf("enumerate worker tenants: %v", err)
			}
			if count != test.want {
				t.Fatalf("tenant count = %d, want %d", count, test.want)
			}
		})
	}
}

func seedClaimableStep(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenantID, workflowID string) {
	t.Helper()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID)
	var versionID, eventID, executionID string
	if err := tx.QueryRow(ctx, `SELECT id FROM workflow_versions
		WHERE tenant_id=$1 AND workflow_id=$2 AND version=1`, tenantID, workflowID).Scan(&versionID); err != nil {
		t.Fatalf("load workflow version: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO events (tenant_id,workflow_id,external_id,event_type,payload)
		VALUES ($1,$2,$3,'test','{}') RETURNING id`, tenantID, workflowID, randomUUID(t, ctx, owner)).Scan(&eventID); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO executions (tenant_id,workflow_id,workflow_version_id,event_id)
		VALUES ($1,$2,$3,$4) RETURNING id`, tenantID, workflowID, versionID, eventID).Scan(&executionID); err != nil {
		t.Fatalf("insert execution: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO step_runs
		(tenant_id,execution_id,step_id,step_type,max_attempts,initial_delay_seconds,max_delay_seconds,timeout_seconds)
		VALUES ($1,$2,'step','noop',3,1,5,5)`, tenantID, executionID); err != nil {
		t.Fatalf("insert step: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit claimable step: %v", err)
	}
}
