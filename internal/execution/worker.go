package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
)

// WorkerConfig bounds worker concurrency and lease behavior.
type WorkerConfig struct {
	Count              int
	PollInterval       time.Duration
	LeaseDuration      time.Duration
	MaxConcurrentSteps int
}

// Worker claims durable steps from PostgreSQL and executes them through the
// connector registry. Execution is at-least-once.
type Worker struct {
	store      *store.Store
	logger     *slog.Logger
	metrics    *observability.Metrics
	connectors Registry
	config     WorkerConfig
}

// DefaultConnectors returns the built-in step types.
func DefaultConnectors(allowedHosts []string, signer *Signer) Registry {
	return Registry{
		"noop": noopConnector,
		"http": NewHTTPConnector(allowedHosts, signer),
	}
}

func NewWorker(repository *store.Store, logger *slog.Logger, metrics *observability.Metrics, connectors Registry, config WorkerConfig) *Worker {
	return &Worker{store: repository, logger: logger, metrics: metrics, connectors: connectors, config: config}
}

func (w *Worker) Run(ctx context.Context) {
	var group sync.WaitGroup
	for index := range w.config.Count {
		group.Go(func() { w.loop(ctx, workerID(index)) })
	}
	group.Wait()
}

func (w *Worker) loop(ctx context.Context, owner string) {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.runOne(ctx, owner)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("worker iteration failed", "worker_id", owner, "error", err)
		}
		if worked && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) runOne(ctx context.Context, owner string) (bool, error) {
	job, found, err := w.store.ClaimStep(ctx, owner, w.config.LeaseDuration, w.config.MaxConcurrentSteps)
	if err != nil || !found {
		return false, err
	}
	started := time.Now()
	stepCtx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds)*time.Second)
	output, err := w.connectors.execute(stepCtx, job)
	cancel()
	w.metrics.StepDuration.WithLabelValues(job.StepType).Observe(time.Since(started).Seconds())
	// Persist the outcome even during shutdown so a finished effect is not re-run.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer persistCancel()
	if err == nil {
		w.metrics.StepsCompleted.WithLabelValues(job.StepType, "succeeded").Inc()
		if err := w.store.CompleteStep(persistCtx, job, output); err != nil {
			return true, fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		w.logger.Info("step succeeded", "execution_id", job.ExecutionID, "step_id", job.StepID, "attempt", job.Attempt)
		return true, nil
	}

	w.metrics.StepsCompleted.WithLabelValues(job.StepType, "failed").Inc()
	message := safeError(err)
	var retryAt *time.Time
	if IsRetryable(err) && job.Attempt < job.MaxAttempts {
		delay := backoff(job.Attempt, time.Duration(job.InitialDelaySeconds)*time.Second, time.Duration(job.MaxDelaySeconds)*time.Second)
		next := time.Now().Add(delay)
		retryAt = &next
	}
	if persistErr := w.store.FailStep(persistCtx, job, message, retryAt); persistErr != nil {
		return true, fmt.Errorf("persist job %s failure: %w", job.ID, persistErr)
	}
	w.logger.Warn("step failed", "execution_id", job.ExecutionID, "step_id", job.StepID,
		"attempt", job.Attempt, "retryable", retryAt != nil, "error", message)
	return true, nil
}

func backoff(attempt int, initial, maximum time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(initial) * math.Pow(2, float64(attempt-1))
	if delay > float64(maximum) {
		return maximum
	}
	return time.Duration(delay)
}

func workerID(index int) string {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "worker-" + strconv.Itoa(index)
	}
	return "worker-" + strconv.Itoa(index) + "-" + hex.EncodeToString(random)
}

func safeError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}
