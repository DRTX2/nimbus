package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ClaimOutbox(ctx context.Context, owner string, lease time.Duration) (OutboxMessage, bool, error) {
	tenants, err := s.workerTenants(ctx, &s.outboxCursor)
	if err != nil {
		return OutboxMessage{}, false, err
	}
	for _, tenantID := range tenants {
		message, found, err := s.claimTenantOutbox(ctx, tenantID, owner, lease)
		if err != nil || found {
			return message, found, err
		}
	}
	return OutboxMessage{}, false, nil
}

func (s *Store) claimTenantOutbox(ctx context.Context, tenantID, owner string, lease time.Duration) (OutboxMessage, bool, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return OutboxMessage{}, false, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var message OutboxMessage
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM outbox_messages
			WHERE published_at IS NULL AND attempt < max_attempts
			  AND available_at <= now() AND (lease_until IS NULL OR lease_until < now())
			ORDER BY available_at, created_at FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE outbox_messages o SET attempt=attempt+1,lease_owner=$1,
			lease_until=now()+($2*interval '1 millisecond'),last_error=NULL
		FROM candidate c WHERE o.id=c.id
		RETURNING o.id,o.tenant_id,o.topic,o.aggregate_id,o.payload,o.attempt,o.max_attempts,o.lease_owner`, owner, lease.Milliseconds()).
		Scan(&message.ID, &message.TenantID, &message.Topic, &message.AggregateID, &message.Payload, &message.Attempt, &message.MaxAttempts, &message.LeaseOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboxMessage{}, false, nil
	}
	if err != nil {
		return OutboxMessage{}, false, fmt.Errorf("claim outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OutboxMessage{}, false, fmt.Errorf("commit outbox claim: %w", err)
	}
	return message, true, nil
}

func (s *Store) PublishOutbox(ctx context.Context, message OutboxMessage) error {
	tx, err := s.beginTenant(ctx, message.TenantID)
	if err != nil {
		return fmt.Errorf("begin outbox publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if message.Topic == "incident.opened" {
		var workflowID string
		err := tx.QueryRow(ctx, `SELECT COALESCE(p.incident_workflow_id::text,'') FROM incidents i
			JOIN monitored_pipelines p ON p.tenant_id=i.tenant_id AND p.id=i.pipeline_id
			WHERE i.tenant_id=$1 AND i.id=$2`, message.TenantID, message.AggregateID).Scan(&workflowID)
		if err != nil {
			return fmt.Errorf("load incident workflow: %w", err)
		}
		if workflowID != "" {
			actor, err := s.systemPrincipal(ctx, tx, message.TenantID)
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"incidentId": message.AggregateID, "source": "nimbus"})
			if _, err := s.IngestEvent(ctx, actor, workflowID, "outbox-"+message.ID, "nimbus.incident.opened", payload, math.MaxInt32); err != nil {
				return fmt.Errorf("dispatch incident workflow: %w", err)
			}
		}
	}
	command, err := tx.Exec(ctx, `UPDATE outbox_messages SET published_at=now(),lease_owner=NULL,lease_until=NULL
		WHERE id=$1 AND lease_owner=$2 AND attempt=$3`, message.ID, message.LeaseOwner, message.Attempt)
	if err != nil {
		return fmt.Errorf("publish outbox: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("outbox lease was lost")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox publish: %w", err)
	}
	return nil
}

func (s *Store) FailOutbox(ctx context.Context, message OutboxMessage, failure string) error {
	tx, err := s.beginTenant(ctx, message.TenantID)
	if err != nil {
		return fmt.Errorf("begin outbox failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	delay := time.Duration(message.Attempt*message.Attempt) * time.Second
	_, err = tx.Exec(ctx, `UPDATE outbox_messages SET available_at=now()+($1*interval '1 millisecond'),last_error=$2,
		lease_owner=NULL,lease_until=NULL WHERE id=$3 AND lease_owner=$4 AND attempt=$5`, delay.Milliseconds(), failure, message.ID, message.LeaseOwner, message.Attempt)
	if err != nil {
		return fmt.Errorf("fail outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox failure: %w", err)
	}
	return nil
}

func (s *Store) systemPrincipal(ctx context.Context, tx pgx.Tx, tenantID string) (Principal, error) {
	var result Principal
	err := tx.QueryRow(ctx, `SELECT principal_id,display_name,membership_role
		FROM public.nimbus_system_principal($1)`, tenantID).Scan(&result.ID, &result.Name, &result.Role)
	if err != nil {
		return Principal{}, fmt.Errorf("load system principal: %w", err)
	}
	result.TenantID = tenantID
	return result, nil
}
