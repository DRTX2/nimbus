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
	assertIncidentQualityLoop(t, ctx, owner, worker, apiStore, apiCtx, principal, tenantA, tenantB)

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
	assertOperationalQueueMetrics(t, ctx, owner, worker, api, tenantA, job.ExecutionID)
	assertWorkerTenantLimit(t, ctx, owner, worker)
}

func assertOperationalQueueMetrics(t *testing.T, ctx context.Context, owner, worker, api *pgxpool.Pool, tenantID, executionID string) {
	t.Helper()
	workerStore := store.New(worker)
	before, err := workerStore.OperationalSnapshot(ctx)
	if err != nil {
		t.Fatalf("query baseline operational metrics as worker: %v", err)
	}
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO step_runs
		(tenant_id,execution_id,step_id,step_type,status,attempt,max_attempts,initial_delay_seconds,max_delay_seconds,timeout_seconds,available_at,lease_owner,lease_until)
		VALUES
		($1,$2,'metrics-due','noop','queued',0,3,1,5,5,now() - interval '2 seconds',NULL,NULL),
		($1,$2,'metrics-delayed','noop','queued',1,3,1,5,5,now() + interval '1 hour',NULL,NULL),
		($1,$2,'metrics-expired','noop','running',1,3,1,5,5,now() - interval '1 hour','expired-worker',now() - interval '2 seconds')`, tenantID, executionID); err != nil {
		t.Fatalf("seed operational steps: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_messages
		(tenant_id,topic,aggregate_type,aggregate_id,payload,idempotency_key,available_at,attempt,max_attempts,lease_owner,lease_until)
		VALUES
		($1,'metrics','test',$2,'{}',gen_random_uuid()::text,now() - interval '2 seconds',0,3,NULL,NULL),
		($1,'metrics','test',$2,'{}',gen_random_uuid()::text,now() + interval '1 hour',1,3,NULL,NULL),
		($1,'metrics','test',$2,'{}',gen_random_uuid()::text,now() - interval '1 hour',3,3,NULL,NULL)`, tenantID, executionID); err != nil {
		t.Fatalf("seed operational outbox: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit operational fixtures: %v", err)
	}
	defer cleanupOperationalFixtures(t, ctx, owner, tenantID, executionID)

	after, err := workerStore.OperationalSnapshot(ctx)
	if err != nil {
		t.Fatalf("query operational metrics as worker: %v", err)
	}
	if after.RunnableSteps != before.RunnableSteps+2 {
		t.Fatalf("runnable steps delta = %d, want 2; delayed retry must be excluded", after.RunnableSteps-before.RunnableSteps)
	}
	if after.OldestRunnableStepAgeSeconds < 0 {
		t.Fatalf("oldest runnable age = %f, want non-negative", after.OldestRunnableStepAgeSeconds)
	}
	if after.ExpiredStepLeases != before.ExpiredStepLeases+1 {
		t.Fatalf("expired lease delta = %d, want 1", after.ExpiredStepLeases-before.ExpiredStepLeases)
	}
	if after.PublishableOutboxMessages != before.PublishableOutboxMessages+1 {
		t.Fatalf("publishable outbox delta = %d, want 1; delayed retry and exhausted message must be excluded", after.PublishableOutboxMessages-before.PublishableOutboxMessages)
	}
	if after.OldestPublishableOutboxAgeSeconds < 0 {
		t.Fatalf("oldest publishable outbox age = %f, want non-negative", after.OldestPublishableOutboxAgeSeconds)
	}
	if after.ExhaustedUnpublishedOutboxMessages != before.ExhaustedUnpublishedOutboxMessages+1 {
		t.Fatalf("exhausted outbox delta = %d, want 1", after.ExhaustedUnpublishedOutboxMessages-before.ExhaustedUnpublishedOutboxMessages)
	}
	ownerSnapshot, err := store.New(owner).OperationalSnapshot(ctx)
	if err != nil {
		t.Fatalf("query operational metrics as migration owner: %v", err)
	}
	if ownerSnapshot.RunnableSteps != after.RunnableSteps ||
		ownerSnapshot.ExpiredStepLeases != after.ExpiredStepLeases ||
		ownerSnapshot.PublishableOutboxMessages != after.PublishableOutboxMessages ||
		ownerSnapshot.ExhaustedUnpublishedOutboxMessages != after.ExhaustedUnpublishedOutboxMessages {
		t.Fatalf("migration owner aggregate differs from worker: owner=%+v worker=%+v", ownerSnapshot, after)
	}
	if _, err := api.Exec(ctx, "SELECT * FROM public.nimbus_operational_queue_metrics()"); err == nil {
		t.Fatal("API role executed worker-only operational metrics function")
	}
	var ownerIsMigrator, publicCanExecute bool
	if err := owner.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner) = current_user,
		has_function_privilege('public', 'public.nimbus_operational_queue_metrics()', 'EXECUTE')
		FROM pg_proc p WHERE p.oid = 'public.nimbus_operational_queue_metrics()'::regprocedure`).Scan(&ownerIsMigrator, &publicCanExecute); err != nil {
		t.Fatalf("inspect operational function privileges: %v", err)
	}
	if !ownerIsMigrator || publicCanExecute {
		t.Fatalf("operational function owner/public execute = %v/%v, want true/false", ownerIsMigrator, publicCanExecute)
	}
}

func cleanupOperationalFixtures(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenantID, executionID string) {
	t.Helper()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Errorf("begin operational fixture cleanup: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		t.Errorf("scope operational fixture cleanup: %v", err)
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM outbox_messages
		WHERE tenant_id=$1 AND aggregate_type='test' AND aggregate_id=$2`, tenantID, executionID); err != nil {
		t.Errorf("clean operational outbox fixtures: %v", err)
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM step_runs
		WHERE tenant_id=$1 AND execution_id=$2 AND step_id LIKE 'metrics-%'`, tenantID, executionID); err != nil {
		t.Errorf("clean operational step fixtures: %v", err)
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE step_runs SET status='succeeded', lease_owner=NULL, lease_until=NULL, finished_at=now()
		WHERE tenant_id=$1 AND execution_id=$2 AND status IN ('queued','running')`, tenantID, executionID); err != nil {
		t.Errorf("finish claim fixture: %v", err)
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE executions SET status='succeeded', finished_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('queued','running')`, tenantID, executionID); err != nil {
		t.Errorf("finish claim execution fixture: %v", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit operational fixture cleanup: %v", err)
	}
}

func assertIncidentQualityLoop(t *testing.T, ctx context.Context, owner, worker *pgxpool.Pool, apiStore *store.Store, apiCtx context.Context, actor store.Principal, tenantA, tenantB string) {
	t.Helper()
	pipelineA, err := apiStore.CreatePipeline(apiCtx, actor, store.Pipeline{
		Name: "Quality A", Mode: "fixture", Region: "us-east-1", QueueARN: "arn:test:quality-a:queue",
		LambdaARN: "arn:test:quality-a:lambda", DLQARN: "arn:test:quality-a:dlq",
	})
	if err != nil {
		t.Fatalf("create quality pipeline A: %v", err)
	}
	ownerStore := store.New(owner)
	pipelineB, err := ownerStore.CreatePipeline(ctx, store.Principal{ID: randomUUID(t, ctx, owner), TenantID: tenantB}, store.Pipeline{
		Name: "Quality B", Mode: "fixture", Region: "us-east-1", QueueARN: "arn:test:quality-b:queue",
		LambdaARN: "arn:test:quality-b:lambda", DLQARN: "arn:test:quality-b:dlq",
	})
	if err != nil {
		t.Fatalf("create quality pipeline B: %v", err)
	}
	now := time.Now().UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	incidentIDs := make([]string, 5)
	for index := range incidentIDs {
		incidentIDs[index] = insertQualityIncident(t, ctx, owner, tenantA, pipelineA.ID, "consumer_stalled", weekStart.Add(time.Duration(index+1)*time.Hour))
	}
	otherTenantIncident := insertQualityIncident(t, ctx, owner, tenantB, pipelineB.ID, "consumer_stalled", weekStart.Add(time.Hour))

	if _, err := apiStore.ReviewIncident(apiCtx, actor, otherTenantIncident, "false_positive", "cross tenant"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant review error = %v, want not found", err)
	}
	for _, review := range []struct {
		incidentID  string
		disposition string
	}{
		{incidentIDs[0], "false_positive"},
		{incidentIDs[0], "true_positive"},
		{incidentIDs[1], "false_positive"},
		{incidentIDs[2], "expected_behavior"},
		{incidentIDs[3], "duplicate"},
	} {
		if _, err := apiStore.ReviewIncident(apiCtx, actor, review.incidentID, review.disposition, "integration review"); err != nil {
			t.Fatalf("review incident as %s: %v", review.disposition, err)
		}
	}
	if _, err := apiStore.ReviewIncident(apiCtx, actor, incidentIDs[3], "duplicate", "integration review"); err != nil {
		t.Fatalf("retry identical incident review: %v", err)
	}

	items, err := apiStore.WeeklyIncidentQuality(apiCtx, tenantA, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("weekly incident quality: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("weekly groups = %d, want 1", len(items))
	}
	item := items[0]
	if item.PipelineID != pipelineA.ID || item.Total != 5 || item.Reviewed != 4 || item.Unreviewed != 1 ||
		item.FalsePositives != 1 || item.TruePositives != 1 || item.ExpectedBehavior != 1 || item.Duplicates != 1 || item.FalsePositiveRate != 0.5 {
		t.Fatalf("unexpected weekly quality item: %+v", item)
	}

	listed, err := apiStore.ListIncidents(apiCtx, tenantA)
	if err != nil {
		t.Fatalf("list reviewed incidents: %v", err)
	}
	var latest *store.IncidentReview
	for _, incident := range listed {
		if incident.ID == incidentIDs[0] {
			latest = incident.Review
		}
	}
	if latest == nil || latest.Disposition != "true_positive" {
		t.Fatalf("latest reclassification = %+v, want true_positive", latest)
	}

	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA)
	var reviews, reviewAudits, reclassificationAudits int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM incident_reviews WHERE tenant_id=$1 AND incident_id=ANY($2::uuid[])`, tenantA, incidentIDs).Scan(&reviews); err != nil {
		t.Fatalf("count incident reviews: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action='incident.review'),count(*) FILTER (WHERE action='incident.reclassify')
		FROM audit_events WHERE tenant_id=$1 AND resource_id=ANY($2::text[])`, tenantA, incidentIDs).Scan(&reviewAudits, &reclassificationAudits); err != nil {
		t.Fatalf("count incident review audits: %v", err)
	}
	if reviews != 5 || reviewAudits != 4 || reclassificationAudits != 1 {
		t.Fatalf("review history/audits = %d/%d/%d, want 5/4/1", reviews, reviewAudits, reclassificationAudits)
	}
	if _, err := worker.Exec(ctx, `INSERT INTO incident_reviews (tenant_id,incident_id,revision,disposition,reviewer_principal_id)
		VALUES ($1,$2,99,'true_positive',$3)`, tenantA, incidentIDs[4], actor.ID); err == nil {
		t.Fatal("worker role inserted an incident review")
	}
}

func insertQualityIncident(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenantID, pipelineID, detector string, openedAt time.Time) string {
	t.Helper()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO incidents
		(tenant_id,pipeline_id,detector,status,fingerprint,severity,summary,evidence,opened_at,recovered_at)
		VALUES ($1,$2,$3,'recovered',gen_random_uuid()::text,'warning','quality fixture','{}',$4,$4) RETURNING id`,
		tenantID, pipelineID, detector, openedAt).Scan(&id); err != nil {
		t.Fatalf("insert quality incident: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit quality incident: %v", err)
	}
	return id
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
