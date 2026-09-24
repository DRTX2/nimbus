package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/david/nimbus/internal/workflow"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetExecution(ctx context.Context, tenantID, executionID string) (Execution, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return Execution{}, fmt.Errorf("begin get execution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result Execution
	err = tx.QueryRow(ctx, `
		SELECT e.id, e.workflow_id, v.version, e.event_id, e.status, e.error, e.started_at, e.finished_at, e.created_at
		FROM executions e
		JOIN workflow_versions v ON v.tenant_id = e.tenant_id AND v.id = e.workflow_version_id
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, executionID).
		Scan(&result.ID, &result.WorkflowID, &result.Version, &result.EventID, &result.Status, &result.Error, &result.StartedAt, &result.FinishedAt, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, fmt.Errorf("get execution: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, step_id, step_type, status, attempt, output, error, started_at, finished_at
		FROM step_runs WHERE tenant_id = $1 AND execution_id = $2 ORDER BY created_at, step_id`, tenantID, executionID)
	if err != nil {
		return Execution{}, fmt.Errorf("list execution steps: %w", err)
	}
	defer rows.Close()
	result.Steps = make([]StepRun, 0)
	for rows.Next() {
		var step StepRun
		if err := rows.Scan(&step.ID, &step.StepID, &step.Type, &step.Status, &step.Attempt, &step.Output, &step.Error, &step.StartedAt, &step.FinishedAt); err != nil {
			return Execution{}, fmt.Errorf("scan execution step: %w", err)
		}
		result.Steps = append(result.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return Execution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Execution{}, fmt.Errorf("commit get execution: %w", err)
	}
	return result, nil
}

// ListExecutions returns executions newest first using a keyset cursor.
func (s *Store) ListExecutions(ctx context.Context, tenantID string, filter ExecutionFilter, page Page) (ExecutionPage, error) {
	key, err := page.key()
	if err != nil {
		return ExecutionPage{}, err
	}
	limit := page.normalizedLimit()
	var afterAt *time.Time
	var afterID *string
	if key != nil {
		afterAt, afterID = &key.At, &key.ID
	}
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return ExecutionPage{}, fmt.Errorf("begin execution list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT e.id, e.workflow_id, w.name, e.status, e.error, e.created_at, e.started_at, e.finished_at
		FROM executions e
		JOIN workflows w ON w.tenant_id = e.tenant_id AND w.id = e.workflow_id
		WHERE e.tenant_id = $1
		  AND ($2 = '' OR e.status = $2)
		  AND ($3 = '' OR e.workflow_id = NULLIF($3, '')::uuid)
		  AND ($4::timestamptz IS NULL OR (e.created_at, e.id) < ($4::timestamptz, $5::uuid))
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $6`, tenantID, filter.Status, filter.WorkflowID, afterAt, afterID, limit+1)
	if err != nil {
		return ExecutionPage{}, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()
	result := ExecutionPage{Items: make([]ExecutionSummary, 0, limit)}
	for rows.Next() {
		var item ExecutionSummary
		if err := rows.Scan(&item.ID, &item.WorkflowID, &item.WorkflowName, &item.Status, &item.Error, &item.CreatedAt, &item.StartedAt, &item.FinishedAt); err != nil {
			return ExecutionPage{}, fmt.Errorf("scan execution: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ExecutionPage{}, fmt.Errorf("iterate executions: %w", err)
	}
	if len(result.Items) > limit {
		last := result.Items[limit-1]
		result.Items = result.Items[:limit]
		result.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionPage{}, fmt.Errorf("commit execution list: %w", err)
	}
	return result, nil
}

func (s *Store) ClaimStep(ctx context.Context, owner string, leaseDuration time.Duration, maxConcurrentSteps int) (Job, bool, error) {
	tenants, err := s.workerTenants(ctx, &s.stepCursor)
	if err != nil {
		return Job{}, false, err
	}
	for _, tenantID := range tenants {
		job, found, err := s.claimTenantStep(ctx, tenantID, owner, leaseDuration, maxConcurrentSteps)
		if err != nil || found {
			return job, found, err
		}
	}
	return Job{}, false, nil
}

func (s *Store) claimTenantStep(ctx context.Context, tenantID, owner string, leaseDuration time.Duration, maxConcurrentSteps int) (Job, bool, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin step claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		WITH exhausted AS (
			UPDATE step_runs
			SET status = 'failed', error = 'worker lease expired after the final attempt',
				finished_at = now(), lease_owner = NULL, lease_until = NULL
			WHERE status = 'running' AND lease_until < now() AND attempt >= max_attempts
			RETURNING tenant_id, execution_id
		)
		UPDATE executions e
		SET status = 'failed', error = 'worker lease expired after the final attempt', finished_at = now()
		FROM exhausted x
		WHERE e.tenant_id = x.tenant_id AND e.id = x.execution_id
		  AND e.status IN ('queued', 'running')`); err != nil {
		return Job{}, false, fmt.Errorf("reap exhausted step leases: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE step_runs s
		SET status = 'cancelled', error = 'cancelled because the execution failed', finished_at = now()
		FROM executions e
		WHERE s.tenant_id = e.tenant_id AND s.execution_id = e.id
		  AND s.status = 'queued' AND e.status = 'failed'`); err != nil {
		return Job{}, false, fmt.Errorf("cancel steps for failed executions: %w", err)
	}

	var job Job
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT s.tenant_id, s.id
			FROM step_runs s
			JOIN executions e ON e.tenant_id = s.tenant_id AND e.id = s.execution_id
			WHERE ((s.status = 'queued' AND s.available_at <= now())
			   OR (s.status = 'running' AND s.lease_until < now()))
			  AND s.attempt < s.max_attempts
			  AND e.status IN ('queued', 'running')
			  AND (SELECT count(*) FROM step_runs WHERE tenant_id = $4 AND status = 'running') < $3
			ORDER BY s.available_at, s.created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE step_runs s
		SET status = 'running', attempt = attempt + 1, lease_owner = $1,
			lease_until = now() + ($2 * interval '1 millisecond'),
			started_at = COALESCE(started_at, now()), error = NULL
		FROM candidate c
		WHERE s.tenant_id = c.tenant_id AND s.id = c.id
		RETURNING s.id, s.tenant_id, s.execution_id, s.step_id, s.step_type, s.config,
			s.attempt, s.max_attempts, s.initial_delay_seconds, s.max_delay_seconds,
			s.timeout_seconds, s.lease_owner,
			(SELECT ev.payload FROM executions ex JOIN events ev ON ev.tenant_id=ex.tenant_id AND ev.id=ex.event_id
			 WHERE ex.tenant_id=s.tenant_id AND ex.id=s.execution_id)`, owner, leaseDuration.Milliseconds(), maxConcurrentSteps, tenantID).
		Scan(&job.ID, &job.TenantID, &job.ExecutionID, &job.StepID, &job.StepType, &job.Config,
			&job.Attempt, &job.MaxAttempts, &job.InitialDelaySeconds, &job.MaxDelaySeconds,
			&job.TimeoutSeconds, &job.LeaseOwner, &job.EventPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, false, fmt.Errorf("commit step reap: %w", err)
		}
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim step: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE executions SET status = 'running', started_at = COALESCE(started_at, now())
		WHERE tenant_id = $1 AND id = $2 AND status = 'queued'`, job.TenantID, job.ExecutionID); err != nil {
		return Job{}, false, fmt.Errorf("start execution: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit step claim: %w", err)
	}
	return job, true, nil
}

func (s *Store) CompleteStep(ctx context.Context, job Job, output json.RawMessage) error {
	tx, err := s.beginTenant(ctx, job.TenantID)
	if err != nil {
		return fmt.Errorf("begin step completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	command, err := tx.Exec(ctx, `
		UPDATE step_runs SET status = 'succeeded', output = $1, finished_at = now(),
			lease_owner = NULL, lease_until = NULL
		WHERE tenant_id = $2 AND id = $3 AND status = 'running'
		  AND lease_owner = $4 AND attempt = $5`,
		output, job.TenantID, job.ID, job.LeaseOwner, job.Attempt)
	if err != nil {
		return fmt.Errorf("complete step: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("step lease was lost before completion")
	}

	var definitionBytes []byte
	var executionStatus string
	err = tx.QueryRow(ctx, `
		SELECT v.definition, e.status
		FROM executions e
		JOIN workflow_versions v ON v.tenant_id = e.tenant_id AND v.id = e.workflow_version_id
		WHERE e.tenant_id = $1 AND e.id = $2
		FOR UPDATE OF e`, job.TenantID, job.ExecutionID).Scan(&definitionBytes, &executionStatus)
	if err != nil {
		return fmt.Errorf("load execution definition: %w", err)
	}
	var definition workflow.Definition
	if err := json.Unmarshal(definitionBytes, &definition); err != nil {
		return fmt.Errorf("decode execution definition: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT step_id, status FROM step_runs WHERE tenant_id = $1 AND execution_id = $2`, job.TenantID, job.ExecutionID)
	if err != nil {
		return fmt.Errorf("load step states: %w", err)
	}
	states := make(map[string]string, len(definition.Steps))
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return fmt.Errorf("scan step state: %w", err)
		}
		states[id] = status
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate step states: %w", err)
	}

	for _, step := range definition.Steps {
		if executionStatus != "running" {
			break
		}
		if _, exists := states[step.ID]; exists {
			continue
		}
		ready := true
		for _, dependency := range step.DependsOn {
			if states[dependency] != "succeeded" {
				ready = false
				break
			}
		}
		if ready {
			if err := insertStep(ctx, tx, job.TenantID, job.ExecutionID, step); err != nil {
				return err
			}
			if err := insertOutbox(ctx, tx, job.TenantID, "step.available", "execution", job.ExecutionID,
				job.ExecutionID+":"+step.ID, map[string]any{"executionId": job.ExecutionID, "stepId": step.ID}); err != nil {
				return err
			}
			states[step.ID] = "queued"
		}
	}

	allSucceeded := len(states) == len(definition.Steps)
	for _, status := range states {
		if status != "succeeded" {
			allSucceeded = false
			break
		}
	}
	if allSucceeded {
		if _, err := tx.Exec(ctx, `
			UPDATE executions SET status = 'succeeded', finished_at = now()
			WHERE tenant_id = $1 AND id = $2 AND status = 'running'`, job.TenantID, job.ExecutionID); err != nil {
			return fmt.Errorf("complete execution: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit step completion: %w", err)
	}
	return nil
}

func (s *Store) FailStep(ctx context.Context, job Job, message string, retryAt *time.Time) error {
	tx, err := s.beginTenant(ctx, job.TenantID)
	if err != nil {
		return fmt.Errorf("begin step failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if retryAt != nil {
		command, err := tx.Exec(ctx, `
			UPDATE step_runs SET status = 'queued', available_at = $1, error = $2,
				lease_owner = NULL, lease_until = NULL
			WHERE tenant_id = $3 AND id = $4 AND status = 'running'
			  AND lease_owner = $5 AND attempt = $6`,
			*retryAt, message, job.TenantID, job.ID, job.LeaseOwner, job.Attempt)
		if err != nil {
			return fmt.Errorf("schedule step retry: %w", err)
		}
		if command.RowsAffected() != 1 {
			return errors.New("step lease was lost before retry")
		}
	} else {
		command, err := tx.Exec(ctx, `
			UPDATE step_runs SET status = 'failed', error = $1, finished_at = now(),
				lease_owner = NULL, lease_until = NULL
			WHERE tenant_id = $2 AND id = $3 AND status = 'running'
			  AND lease_owner = $4 AND attempt = $5`,
			message, job.TenantID, job.ID, job.LeaseOwner, job.Attempt)
		if err != nil {
			return fmt.Errorf("fail step: %w", err)
		}
		if command.RowsAffected() != 1 {
			return errors.New("step lease was lost before failure")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE executions SET status = 'failed', error = $1, finished_at = now()
			WHERE tenant_id = $2 AND id = $3 AND status IN ('queued', 'running')`, message, job.TenantID, job.ExecutionID); err != nil {
			return fmt.Errorf("fail execution: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE step_runs SET status = 'cancelled', error = 'cancelled because another step failed', finished_at = now()
			WHERE tenant_id = $1 AND execution_id = $2 AND status = 'queued'`, job.TenantID, job.ExecutionID); err != nil {
			return fmt.Errorf("cancel queued execution steps: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit step failure: %w", err)
	}
	return nil
}

func (s *Store) Analytics(ctx context.Context, tenantID string) (Analytics, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return Analytics{}, fmt.Errorf("begin analytics: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result Analytics
	err = tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM events WHERE tenant_id = $1 AND received_at >= now() - interval '24 hours'),
			(SELECT count(*) FROM executions WHERE tenant_id = $1 AND created_at >= now() - interval '24 hours'),
			(SELECT count(*) FROM executions WHERE tenant_id = $1 AND status = 'succeeded' AND created_at >= now() - interval '24 hours'),
			(SELECT count(*) FROM executions WHERE tenant_id = $1 AND status = 'failed' AND created_at >= now() - interval '24 hours'),
			(SELECT count(*) FROM step_runs WHERE tenant_id = $1 AND status = 'queued'),
			COALESCE((SELECT extract(epoch FROM now() - min(available_at))::bigint FROM step_runs WHERE tenant_id = $1 AND status = 'queued'), 0)`, tenantID).
		Scan(&result.Events24h, &result.Executions24h, &result.Succeeded24h, &result.Failed24h, &result.QueuedSteps, &result.OldestQueuedSeconds)
	if err != nil {
		return Analytics{}, fmt.Errorf("query analytics: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Analytics{}, fmt.Errorf("commit analytics: %w", err)
	}
	finished := result.Succeeded24h + result.Failed24h
	if finished > 0 {
		result.SuccessRate = float64(result.Succeeded24h) / float64(finished)
	}
	return result, nil
}
