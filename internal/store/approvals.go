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

func (s *Store) ListApprovals(ctx context.Context, tenantID string) ([]Approval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, execution_id, status, requested_by, decided_by, reason, expires_at, decided_at, created_at
		FROM approval_requests WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 100`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()
	result := make([]Approval, 0)
	for rows.Next() {
		var item Approval
		if err := rows.Scan(&item.ID, &item.ExecutionID, &item.Status, &item.RequestedBy, &item.DecidedBy, &item.Reason, &item.ExpiresAt, &item.DecidedAt, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) DecideApproval(ctx context.Context, actor Principal, approvalID, decision, comment string) (Approval, error) {
	if decision != "approved" && decision != "rejected" {
		return Approval{}, errors.New("decision must be approved or rejected")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Approval{}, fmt.Errorf("begin approval decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result Approval
	err = tx.QueryRow(ctx, `
		SELECT id, execution_id, status, requested_by, decided_by, reason, expires_at, decided_at, created_at
		FROM approval_requests WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, actor.TenantID, approvalID).
		Scan(&result.ID, &result.ExecutionID, &result.Status, &result.RequestedBy, &result.DecidedBy, &result.Reason, &result.ExpiresAt, &result.DecidedAt, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, fmt.Errorf("load approval: %w", err)
	}
	if result.RequestedBy == actor.ID {
		return Approval{}, fmt.Errorf("%w: requester cannot decide their own approval", ErrForbidden)
	}
	if result.Status != "pending" {
		if result.Status == decision {
			return result, nil
		}
		return Approval{}, ErrConflict
	}
	if time.Now().After(result.ExpiresAt) {
		_, _ = tx.Exec(ctx, `UPDATE approval_requests SET status = 'expired' WHERE tenant_id = $1 AND id = $2`, actor.TenantID, approvalID)
		_, _ = tx.Exec(ctx, `UPDATE executions SET status = 'cancelled', finished_at = now() WHERE tenant_id = $1 AND id = $2`, actor.TenantID, result.ExecutionID)
		if err := tx.Commit(ctx); err != nil {
			return Approval{}, fmt.Errorf("expire approval: %w", err)
		}
		return Approval{}, ErrConflict
	}

	now := time.Now().UTC()
	result.Status, result.DecidedBy, result.DecidedAt = decision, &actor.ID, &now
	if _, err := tx.Exec(ctx, `
		UPDATE approval_requests SET status = $1, decided_by = $2, decision_comment = $3, decided_at = $4
		WHERE tenant_id = $5 AND id = $6`, decision, actor.ID, comment, now, actor.TenantID, approvalID); err != nil {
		return Approval{}, fmt.Errorf("update approval: %w", err)
	}
	if decision == "rejected" {
		if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'cancelled', finished_at = now(), error = 'approval rejected' WHERE tenant_id = $1 AND id = $2`, actor.TenantID, result.ExecutionID); err != nil {
			return Approval{}, fmt.Errorf("cancel rejected execution: %w", err)
		}
	} else {
		var definitionBytes []byte
		err := tx.QueryRow(ctx, `
			SELECT v.definition FROM executions e
			JOIN workflow_versions v ON v.tenant_id = e.tenant_id AND v.id = e.workflow_version_id
			WHERE e.tenant_id = $1 AND e.id = $2 AND e.status = 'awaiting_approval'
			FOR UPDATE OF e`, actor.TenantID, result.ExecutionID).Scan(&definitionBytes)
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrConflict
		}
		if err != nil {
			return Approval{}, fmt.Errorf("load approved execution: %w", err)
		}
		var definition workflow.Definition
		if err := json.Unmarshal(definitionBytes, &definition); err != nil {
			return Approval{}, fmt.Errorf("decode approved workflow: %w", err)
		}
		for _, step := range definition.Steps {
			if len(step.DependsOn) == 0 {
				if err := insertStep(ctx, tx, actor.TenantID, result.ExecutionID, step); err != nil {
					return Approval{}, err
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'queued' WHERE tenant_id = $1 AND id = $2`, actor.TenantID, result.ExecutionID); err != nil {
			return Approval{}, fmt.Errorf("queue approved execution: %w", err)
		}
		if err := insertOutbox(ctx, tx, actor.TenantID, "step.available", "execution", result.ExecutionID, result.ExecutionID+":approved", map[string]any{"executionId": result.ExecutionID}); err != nil {
			return Approval{}, err
		}
	}
	if err := insertAudit(ctx, tx, actor, "approval.decide", "approval", approvalID, "succeeded", map[string]any{"decision": decision}); err != nil {
		return Approval{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, fmt.Errorf("commit approval decision: %w", err)
	}
	return result, nil
}
