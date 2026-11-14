package awsmonitor

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
)

const (
	// Jitter stays well under the five-minute window so every check still
	// targets the window that closed at the preceding boundary.
	maxCheckJitter  = 2 * time.Minute
	maxChecksPerRun = 100
	checkTimeout    = 15 * time.Second
)

type checkStore interface {
	ClaimPipelineCheck(ctx context.Context, jitter time.Duration, maxPerHour int) (store.PipelineCheck, bool, error)
	RecordObservation(ctx context.Context, actor store.Principal, observation store.Observation) ([]store.Incident, error)
}

type pipelineCollector interface {
	Collect(ctx context.Context, pipeline store.Pipeline) (store.Observation, error)
}

// Scheduler checks every active AWS pipeline once per closed five-minute
// window. PostgreSQL claims make it safe to run on every worker replica.
type Scheduler struct {
	store            checkStore
	collector        pipelineCollector
	logger           *slog.Logger
	metrics          *observability.Metrics
	interval         time.Duration
	maxChecksPerHour int
}

func NewScheduler(repository checkStore, collector pipelineCollector, logger *slog.Logger, metrics *observability.Metrics, interval time.Duration, maxChecksPerHour int) *Scheduler {
	return &Scheduler{store: repository, collector: collector, logger: logger, metrics: metrics, interval: interval, maxChecksPerHour: maxChecksPerHour}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		s.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce checks due pipelines until none remain or the per-run bound is hit.
func (s *Scheduler) RunOnce(ctx context.Context) {
	for range maxChecksPerRun {
		if ctx.Err() != nil {
			return
		}
		claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		check, found, err := s.store.ClaimPipelineCheck(claimCtx, rand.N(maxCheckJitter), s.maxChecksPerHour)
		cancel()
		if err != nil {
			s.logger.Warn("pipeline check claim failed", "error", err)
			return
		}
		if !found {
			return
		}
		s.metrics.PipelineChecks.WithLabelValues(s.check(ctx, check)).Inc()
	}
}

func (s *Scheduler) check(ctx context.Context, check store.PipelineCheck) string {
	if !check.Allowed {
		s.logger.Warn("pipeline check skipped: tenant CloudWatch budget exhausted", "tenant_id", check.Actor.TenantID, "pipeline_id", check.Pipeline.ID)
		return "budget_exhausted"
	}
	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	observation, err := s.collector.Collect(checkCtx, check.Pipeline)
	if err != nil {
		s.logger.Warn("scheduled pipeline check failed", "pipeline_id", check.Pipeline.ID, "reason", FailureReason(err), "error", err)
		return "failed"
	}
	if _, err := s.store.RecordObservation(checkCtx, check.Actor, observation); err != nil {
		s.logger.Error("scheduled observation could not be evaluated", "pipeline_id", check.Pipeline.ID, "error", err)
		return "failed"
	}
	return "observed"
}
