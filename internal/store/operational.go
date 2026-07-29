package store

import (
	"context"
	"fmt"

	"github.com/david/nimbus/internal/platform/observability"
)

func (s *Store) OperationalSnapshot(ctx context.Context) (observability.OperationalSnapshot, error) {
	var snapshot observability.OperationalSnapshot
	err := s.pool.QueryRow(ctx, `SELECT runnable_steps, oldest_runnable_step_age_seconds,
		expired_step_leases, publishable_outbox_messages, oldest_publishable_outbox_age_seconds,
		exhausted_unpublished_outbox_messages FROM public.nimbus_operational_queue_metrics()`).Scan(
		&snapshot.RunnableSteps, &snapshot.OldestRunnableStepAgeSeconds, &snapshot.ExpiredStepLeases,
		&snapshot.PublishableOutboxMessages, &snapshot.OldestPublishableOutboxAgeSeconds,
		&snapshot.ExhaustedUnpublishedOutboxMessages)
	if err != nil {
		return observability.OperationalSnapshot{}, fmt.Errorf("query operational queue metrics: %w", err)
	}
	return snapshot, nil
}
