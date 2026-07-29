package execution

import (
	"context"
	"log/slog"
	"time"

	"github.com/david/nimbus/internal/store"
)

type OutboxPublisher struct {
	store  *store.Store
	logger *slog.Logger
	poll   time.Duration
	lease  time.Duration
}

func NewOutboxPublisher(repository *store.Store, logger *slog.Logger, poll, lease time.Duration) *OutboxPublisher {
	return &OutboxPublisher{store: repository, logger: logger, poll: poll, lease: lease}
}

func (p *OutboxPublisher) Run(ctx context.Context) {
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()
	owner := workerID(99)
	for {
		message, found, err := p.store.ClaimOutbox(ctx, owner, p.lease)
		if err != nil {
			p.logger.Error("outbox claim failed", "error", err)
		} else if found {
			if err := p.store.PublishOutbox(ctx, message); err != nil {
				p.logger.Warn("outbox publish failed", "topic", message.Topic, "attempt", message.Attempt, "error", err)
				_ = p.store.FailOutbox(context.WithoutCancel(ctx), message, safeError(err))
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
