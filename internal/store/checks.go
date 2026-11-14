package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PipelineCheck is a claimed scheduled CloudWatch check. Allowed is false when
// the tenant budget is spent; the window is consumed either way.
type PipelineCheck struct {
	Pipeline Pipeline
	Actor    Principal
	Allowed  bool
}

// ReserveAWSCheck spends one CloudWatch check from the tenant's hourly budget.
// Attempts are counted before calling AWS so failing calls are bounded too.
func (s *Store) ReserveAWSCheck(ctx context.Context, actor Principal, pipelineID string, maxPerHour int) error {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return fmt.Errorf("begin AWS check reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	allowed, err := reserveAWSCheck(ctx, tx, actor, pipelineID, maxPerHour)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRateLimitExceeded
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit AWS check reservation: %w", err)
	}
	return nil
}

// ClaimPipelineCheck claims the next active AWS pipeline whose check is due,
// moves its next check past the following five-minute boundary by jitter, and
// spends one check from the tenant budget on behalf of the system principal.
func (s *Store) ClaimPipelineCheck(ctx context.Context, jitter time.Duration, maxPerHour int) (PipelineCheck, bool, error) {
	rows, err := s.pool.Query(ctx, "SELECT tenant_id FROM public.nimbus_due_check_tenants(100)")
	if err != nil {
		return PipelineCheck{}, false, fmt.Errorf("enumerate due check tenants: %w", err)
	}
	tenants, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return PipelineCheck{}, false, fmt.Errorf("scan due check tenants: %w", err)
	}
	for _, tenantID := range tenants {
		check, found, err := s.claimTenantPipelineCheck(ctx, tenantID, jitter, maxPerHour)
		if err != nil || found {
			return check, found, err
		}
	}
	return PipelineCheck{}, false, nil
}

func (s *Store) claimTenantPipelineCheck(ctx context.Context, tenantID string, jitter time.Duration, maxPerHour int) (PipelineCheck, bool, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return PipelineCheck{}, false, fmt.Errorf("begin pipeline check claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var check PipelineCheck
	item := &check.Pipeline
	err = tx.QueryRow(ctx, `
		WITH due AS (
			SELECT id FROM monitored_pipelines
			WHERE tenant_id=$1 AND mode='aws' AND status='active' AND (next_check_at IS NULL OR next_check_at <= now())
			ORDER BY next_check_at NULLS FIRST, id FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE monitored_pipelines p
		SET next_check_at=date_bin('5 minutes', now(), timestamptz 'epoch') + interval '5 minutes' + $2*interval '1 millisecond'
		FROM due WHERE p.tenant_id=$1 AND p.id=due.id
		RETURNING p.id,p.name,p.mode,p.region,p.queue_arn,p.lambda_arn,p.dlq_arn,COALESCE(p.role_arn,''),COALESCE(p.external_id_ref,''),
			COALESCE(p.incident_workflow_id::text,''),COALESCE(p.recovery_workflow_id::text,''),p.status,p.consecutive_windows,p.backlog_threshold,p.age_threshold_seconds,p.created_at`,
		tenantID, jitter.Milliseconds()).Scan(&item.ID, &item.Name, &item.Mode, &item.Region,
		&item.QueueARN, &item.LambdaARN, &item.DLQARN, &item.RoleARN, &item.ExternalIDRef, &item.IncidentWorkflowID, &item.RecoveryWorkflowID, &item.Status, &item.ConsecutiveWindows,
		&item.BacklogThreshold, &item.AgeThresholdSeconds, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PipelineCheck{}, false, nil
	}
	if err != nil {
		return PipelineCheck{}, false, fmt.Errorf("claim pipeline check: %w", err)
	}
	if check.Actor, err = s.systemPrincipal(ctx, tx, tenantID); err != nil {
		return PipelineCheck{}, false, err
	}
	if check.Allowed, err = reserveAWSCheck(ctx, tx, check.Actor, item.ID, maxPerHour); err != nil {
		return PipelineCheck{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PipelineCheck{}, false, fmt.Errorf("commit pipeline check claim: %w", err)
	}
	return check, true, nil
}

func reserveAWSCheck(ctx context.Context, tx pgx.Tx, actor Principal, pipelineID string, maxPerHour int) (bool, error) {
	// Serializes concurrent reservations so parallel requests cannot overspend.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('nimbus.aws_check:' || $1::text, 0))`, actor.TenantID); err != nil {
		return false, fmt.Errorf("lock AWS check budget: %w", err)
	}
	var recentChecks int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND action = 'pipeline.check' AND occurred_at >= now() - interval '1 hour'`, actor.TenantID).Scan(&recentChecks); err != nil {
		return false, fmt.Errorf("check AWS check budget: %w", err)
	}
	if recentChecks >= maxPerHour {
		return false, nil
	}
	if err := insertAudit(ctx, tx, actor, "pipeline.check", "pipeline", pipelineID, "succeeded", map[string]any{}); err != nil {
		return false, err
	}
	return true, nil
}
