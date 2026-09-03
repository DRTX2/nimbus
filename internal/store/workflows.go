package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/david/nimbus/internal/workflow"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")
var ErrEventTypeMismatch = errors.New("event type does not match workflow trigger")
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

func (s *Store) CreateWorkflow(ctx context.Context, actor Principal, name string, approvalRequired bool, definition workflow.Definition) (Workflow, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return Workflow{}, fmt.Errorf("encode workflow definition: %w", err)
	}
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return Workflow{}, fmt.Errorf("begin create workflow: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result Workflow
	err = tx.QueryRow(ctx, `
		INSERT INTO workflows (tenant_id, name, approval_required)
		VALUES ($1, $2, $3)
		RETURNING id, name, status, active_version, created_at, updated_at, approval_required`, actor.TenantID, name, approvalRequired).
		Scan(&result.ID, &result.Name, &result.Status, &result.Version, &result.CreatedAt, &result.UpdatedAt, &result.ApprovalRequired)
	if err != nil {
		return Workflow{}, fmt.Errorf("insert workflow: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_versions (tenant_id, workflow_id, version, definition)
		VALUES ($1, $2, 1, $3)`, actor.TenantID, result.ID, encoded); err != nil {
		return Workflow{}, fmt.Errorf("insert workflow version: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "workflow.create", "workflow", result.ID, "succeeded", map[string]any{"approvalRequired": approvalRequired}); err != nil {
		return Workflow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Workflow{}, fmt.Errorf("commit workflow: %w", err)
	}
	result.Definition = definition
	return result, nil
}

func (s *Store) ListWorkflows(ctx context.Context, tenantID string) ([]Workflow, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin workflow list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT w.id, w.name, w.status, w.active_version, v.definition, w.created_at, w.updated_at, w.approval_required
		FROM workflows w
		JOIN workflow_versions v ON v.tenant_id = w.tenant_id
		 AND v.workflow_id = w.id AND v.version = w.active_version
		WHERE w.tenant_id = $1
		ORDER BY w.created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	result := make([]Workflow, 0)
	for rows.Next() {
		var item Workflow
		var definition []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Version, &definition, &item.CreatedAt, &item.UpdatedAt, &item.ApprovalRequired); err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		if err := json.Unmarshal(definition, &item.Definition); err != nil {
			return nil, fmt.Errorf("decode stored workflow %s: %w", item.ID, err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit workflow list: %w", err)
	}
	return result, nil
}

func (s *Store) CreateWorkflowVersion(ctx context.Context, actor Principal, workflowID string, definition workflow.Definition) (Workflow, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return Workflow{}, fmt.Errorf("encode workflow version: %w", err)
	}
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return Workflow{}, fmt.Errorf("begin workflow version: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists string
	if err := tx.QueryRow(ctx, `SELECT id FROM workflows WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, actor.TenantID, workflowID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return Workflow{}, ErrNotFound
	} else if err != nil {
		return Workflow{}, fmt.Errorf("lock workflow: %w", err)
	}
	var version int
	err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM workflow_versions WHERE tenant_id=$1 AND workflow_id=$2`, actor.TenantID, workflowID).Scan(&version)
	if err != nil {
		return Workflow{}, fmt.Errorf("allocate workflow version: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_versions (tenant_id,workflow_id,version,definition) VALUES ($1,$2,$3,$4)`, actor.TenantID, workflowID, version, encoded); err != nil {
		return Workflow{}, fmt.Errorf("insert workflow version: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "workflow.version.create", "workflow", workflowID, "succeeded", map[string]any{"version": version}); err != nil {
		return Workflow{}, err
	}
	result := Workflow{ID: workflowID, Version: version, Definition: definition}
	if err := tx.QueryRow(ctx, `SELECT name,status,approval_required,created_at,updated_at FROM workflows WHERE tenant_id=$1 AND id=$2`, actor.TenantID, workflowID).
		Scan(&result.Name, &result.Status, &result.ApprovalRequired, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return Workflow{}, fmt.Errorf("load workflow version response: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Workflow{}, fmt.Errorf("commit workflow version: %w", err)
	}
	return result, nil
}

func (s *Store) ActivateWorkflowVersion(ctx context.Context, actor Principal, workflowID string, version int) error {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE workflows w SET active_version=$1,updated_at=now() WHERE tenant_id=$2 AND id=$3
		AND EXISTS(SELECT 1 FROM workflow_versions v WHERE v.tenant_id=w.tenant_id AND v.workflow_id=w.id AND v.version=$1)`, version, actor.TenantID, workflowID)
	if err != nil {
		return fmt.Errorf("activate workflow version: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor, "workflow.version.activate", "workflow", workflowID, "succeeded", map[string]any{"version": version}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) IngestEvent(ctx context.Context, actor Principal, workflowID, externalID, eventType string, payload json.RawMessage, maxIngestionRate int) (IngestResult, error) {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return IngestResult{}, fmt.Errorf("begin event ingestion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var versionID string
	var definitionBytes []byte
	var approvalRequired bool
	err = tx.QueryRow(ctx, `
		SELECT v.id, v.definition, w.approval_required
		FROM workflows w
		JOIN workflow_versions v ON v.tenant_id = w.tenant_id
		 AND v.workflow_id = w.id AND v.version = w.active_version
		WHERE w.tenant_id = $1 AND w.id = $2 AND w.status = 'active'
		FOR SHARE OF w`, actor.TenantID, workflowID).Scan(&versionID, &definitionBytes, &approvalRequired)
	if errors.Is(err, pgx.ErrNoRows) {
		return IngestResult{}, ErrNotFound
	}
	if err != nil {
		return IngestResult{}, fmt.Errorf("load active workflow: %w", err)
	}
	var definition workflow.Definition
	if err := json.Unmarshal(definitionBytes, &definition); err != nil {
		return IngestResult{}, fmt.Errorf("decode workflow definition: %w", err)
	}
	if eventType != definition.EventType {
		return IngestResult{}, fmt.Errorf("%w: got %q, want %q", ErrEventTypeMismatch, eventType, definition.EventType)
	}

	var recentEvents int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM events WHERE tenant_id = $1 AND received_at >= now() - interval '1 minute'`, actor.TenantID).Scan(&recentEvents); err != nil {
		return IngestResult{}, fmt.Errorf("check ingestion rate: %w", err)
	}
	if recentEvents >= maxIngestionRate {
		return IngestResult{}, ErrRateLimitExceeded
	}

	var eventID string
	err = tx.QueryRow(ctx, `
		INSERT INTO events (tenant_id, workflow_id, external_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, workflow_id, external_id) DO NOTHING
		RETURNING id`, actor.TenantID, workflowID, externalID, eventType, payload).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		var duplicate IngestResult
		err = tx.QueryRow(ctx, `
			SELECT e.id, x.id
			FROM events e
			JOIN executions x ON x.tenant_id = e.tenant_id AND x.event_id = e.id
			WHERE e.tenant_id = $1 AND e.workflow_id = $2 AND e.external_id = $3`, actor.TenantID, workflowID, externalID).
			Scan(&duplicate.EventID, &duplicate.ExecutionID)
		if err != nil {
			return IngestResult{}, fmt.Errorf("load duplicate event: %w", err)
		}
		duplicate.Duplicate = true
		if err := tx.Commit(ctx); err != nil {
			return IngestResult{}, fmt.Errorf("commit duplicate event: %w", err)
		}
		return duplicate, nil
	}
	if err != nil {
		return IngestResult{}, fmt.Errorf("insert event: %w", err)
	}

	var executionID string
	status := "queued"
	if approvalRequired {
		status = "awaiting_approval"
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO executions (tenant_id, workflow_id, workflow_version_id, event_id, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, actor.TenantID, workflowID, versionID, eventID, status).Scan(&executionID)
	if err != nil {
		return IngestResult{}, fmt.Errorf("insert execution: %w", err)
	}
	result := IngestResult{EventID: eventID, ExecutionID: executionID}
	if approvalRequired {
		if err := tx.QueryRow(ctx, `
			INSERT INTO approval_requests (tenant_id, execution_id, requested_by, reason)
			VALUES ($1, $2, $3, 'Workflow policy requires approval') RETURNING id`, actor.TenantID, executionID, actor.ID).Scan(&result.ApprovalID); err != nil {
			return IngestResult{}, fmt.Errorf("create approval request: %w", err)
		}
	} else {
		for _, step := range definition.Steps {
			if len(step.DependsOn) == 0 {
				if err := insertStep(ctx, tx, actor.TenantID, executionID, step); err != nil {
					return IngestResult{}, err
				}
			}
		}
		if err := insertOutbox(ctx, tx, actor.TenantID, "step.available", "execution", executionID, executionID+":roots", map[string]any{"executionId": executionID}); err != nil {
			return IngestResult{}, err
		}
	}
	if err := insertAudit(ctx, tx, actor, "event.ingest", "execution", executionID, "succeeded", map[string]any{"approvalRequired": approvalRequired}); err != nil {
		return IngestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, fmt.Errorf("commit event ingestion: %w", err)
	}
	return result, nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, tenantID, topic, aggregateType, aggregateID, key string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_messages (tenant_id, topic, aggregate_type, aggregate_id, payload, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (idempotency_key) DO NOTHING`,
		tenantID, topic, aggregateType, aggregateID, encoded, key); err != nil {
		return fmt.Errorf("insert outbox message: %w", err)
	}
	return nil
}

func insertStep(ctx context.Context, tx pgx.Tx, tenantID, executionID string, step workflow.Step) error {
	config, err := json.Marshal(step.HTTP)
	if err != nil {
		return fmt.Errorf("encode step %s: %w", step.ID, err)
	}
	dependencies, err := json.Marshal(step.DependsOn)
	if err != nil {
		return fmt.Errorf("encode dependencies for %s: %w", step.ID, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO step_runs (
			tenant_id, execution_id, step_id, step_type, config, depends_on,
			max_attempts, initial_delay_seconds, max_delay_seconds, timeout_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, execution_id, step_id) DO NOTHING`,
		tenantID, executionID, step.ID, step.Type, config, dependencies,
		step.Retry.MaxAttempts, step.Retry.InitialDelaySeconds, step.Retry.MaxDelaySeconds, step.TimeoutSeconds)
	if err != nil {
		return fmt.Errorf("insert step %s: %w", step.ID, err)
	}
	return nil
}
