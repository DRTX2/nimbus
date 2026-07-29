package observability

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const operationalRefreshTimeout = 5 * time.Second

type OperationalSnapshot struct {
	RunnableSteps                      int64
	OldestRunnableStepAgeSeconds       float64
	ExpiredStepLeases                  int64
	PublishableOutboxMessages          int64
	OldestPublishableOutboxAgeSeconds  float64
	ExhaustedUnpublishedOutboxMessages int64
}

type OperationalSource interface {
	OperationalSnapshot(context.Context) (OperationalSnapshot, error)
}

type OperationalRefresher struct {
	source   OperationalSource
	metrics  *Metrics
	logger   *slog.Logger
	interval time.Duration
}

func NewOperationalRefresher(source OperationalSource, metrics *Metrics, logger *slog.Logger, interval time.Duration) *OperationalRefresher {
	return &OperationalRefresher{source: source, metrics: metrics, logger: logger, interval: interval}
}

func (r *OperationalRefresher) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		r.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *OperationalRefresher) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, operationalRefreshTimeout)
	defer cancel()
	snapshot, err := r.source.OperationalSnapshot(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logger.Warn("operational metrics refresh failed",
				"source", "postgresql", "timed_out", errors.Is(err, context.DeadlineExceeded))
		}
		return
	}
	r.metrics.RunnableSteps.Set(float64(snapshot.RunnableSteps))
	r.metrics.OldestRunnableStepAgeSeconds.Set(nonnegative(snapshot.OldestRunnableStepAgeSeconds))
	r.metrics.ExpiredStepLeases.Set(float64(snapshot.ExpiredStepLeases))
	r.metrics.PublishableOutboxMessages.Set(float64(snapshot.PublishableOutboxMessages))
	r.metrics.OldestPublishableOutboxAgeSeconds.Set(nonnegative(snapshot.OldestPublishableOutboxAgeSeconds))
	r.metrics.ExhaustedUnpublishedOutboxMessages.Set(float64(snapshot.ExhaustedUnpublishedOutboxMessages))
	r.metrics.OperationalMetricsLastSuccess.Set(float64(time.Now().Unix()))
}

func nonnegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
