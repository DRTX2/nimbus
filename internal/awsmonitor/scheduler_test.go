package awsmonitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
	dto "github.com/prometheus/client_model/go"
)

type fakeCheckStore struct {
	checks   []store.PipelineCheck
	jitters  []time.Duration
	recorded []store.Observation
}

func (f *fakeCheckStore) ClaimPipelineCheck(_ context.Context, jitter time.Duration, _ int) (store.PipelineCheck, bool, error) {
	f.jitters = append(f.jitters, jitter)
	if len(f.checks) == 0 {
		return store.PipelineCheck{}, false, nil
	}
	check := f.checks[0]
	f.checks = f.checks[1:]
	return check, true, nil
}

func (f *fakeCheckStore) RecordObservation(_ context.Context, _ store.Principal, observation store.Observation) ([]store.Incident, error) {
	f.recorded = append(f.recorded, observation)
	return nil, nil
}

type fakeCollector struct{ failFor string }

func (f fakeCollector) Collect(_ context.Context, pipeline store.Pipeline) (store.Observation, error) {
	if pipeline.ID == f.failFor {
		return store.Observation{}, errors.New("AssumeRole denied")
	}
	return store.Observation{PipelineID: pipeline.ID}, nil
}

func TestSchedulerRecordsOnlyAllowedSuccessfulChecks(t *testing.T) {
	repository := &fakeCheckStore{checks: []store.PipelineCheck{
		{Pipeline: store.Pipeline{ID: "ok"}, Allowed: true},
		{Pipeline: store.Pipeline{ID: "over-budget"}, Allowed: false},
		{Pipeline: store.Pipeline{ID: "denied"}, Allowed: true},
	}}
	metrics := observability.NewMetrics(false)
	scheduler := NewScheduler(repository, fakeCollector{failFor: "denied"}, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics, time.Minute, 60)

	scheduler.RunOnce(context.Background())

	if len(repository.recorded) != 1 || repository.recorded[0].PipelineID != "ok" {
		t.Fatalf("recorded = %+v, want only pipeline ok", repository.recorded)
	}
	for outcome, want := range map[string]float64{"observed": 1, "budget_exhausted": 1, "failed": 1} {
		var counter dto.Metric
		if err := metrics.PipelineChecks.WithLabelValues(outcome).Write(&counter); err != nil {
			t.Fatal(err)
		}
		if got := counter.GetCounter().GetValue(); got != want {
			t.Errorf("%s checks = %v, want %v", outcome, got, want)
		}
	}
	for _, jitter := range repository.jitters {
		if jitter < 0 || jitter >= maxCheckJitter {
			t.Errorf("jitter %v outside [0, %v)", jitter, maxCheckJitter)
		}
	}
}

func TestSchedulerRunIsBounded(t *testing.T) {
	repository := &fakeCheckStore{}
	for range maxChecksPerRun + 5 {
		repository.checks = append(repository.checks, store.PipelineCheck{Pipeline: store.Pipeline{ID: "p"}, Allowed: true})
	}
	scheduler := NewScheduler(repository, fakeCollector{}, slog.New(slog.NewTextHandler(io.Discard, nil)), observability.NewMetrics(false), time.Minute, 60)

	scheduler.RunOnce(context.Background())

	if len(repository.recorded) != maxChecksPerRun {
		t.Fatalf("recorded %d checks, want the per-run bound %d", len(repository.recorded), maxChecksPerRun)
	}
}
