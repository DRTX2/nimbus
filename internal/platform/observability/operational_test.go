package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeOperationalSource struct {
	mu       sync.Mutex
	snapshot OperationalSnapshot
	err      error
	calls    int
}

func (f *fakeOperationalSource) OperationalSnapshot(context.Context) (OperationalSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.snapshot, f.err
}

func (f *fakeOperationalSource) set(snapshot OperationalSnapshot, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = snapshot
	f.err = err
}

func TestOperationalRefresherRetainsLastGoodSnapshot(t *testing.T) {
	source := &fakeOperationalSource{snapshot: OperationalSnapshot{
		RunnableSteps: 7, OldestRunnableStepAgeSeconds: -2, ExpiredStepLeases: 3,
		PublishableOutboxMessages: 5, OldestPublishableOutboxAgeSeconds: 11,
		ExhaustedUnpublishedOutboxMessages: 2,
	}}
	metrics := NewMetrics(true)
	refresher := NewOperationalRefresher(source, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)
	refresher.refresh(context.Background())
	assertMetric(t, metrics, "nimbus_runnable_steps 7")
	assertMetric(t, metrics, "nimbus_oldest_runnable_step_age_seconds 0")
	assertMetric(t, metrics, "nimbus_expired_step_leases 3")
	assertMetric(t, metrics, "nimbus_publishable_outbox_messages 5")
	assertMetric(t, metrics, "nimbus_oldest_publishable_outbox_age_seconds 11")
	assertMetric(t, metrics, "nimbus_exhausted_unpublished_outbox_messages 2")
	assertMetricPresent(t, metrics, "nimbus_operational_metrics_last_success_timestamp_seconds ")

	source.set(OperationalSnapshot{}, errors.New("database unavailable"))
	refresher.refresh(context.Background())
	assertMetric(t, metrics, "nimbus_runnable_steps 7")
	assertMetric(t, metrics, "nimbus_exhausted_unpublished_outbox_messages 2")
}

func TestOperationalRefresherStopsWithContext(t *testing.T) {
	source := &fakeOperationalSource{}
	refresher := NewOperationalRefresher(source, NewMetrics(true), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		refresher.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresher did not stop after cancellation")
	}
}

func TestOperationalMetricsAreDisabledForAPIOnlyProcess(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewMetrics(false).Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(recorder.Body.String(), "nimbus_runnable_steps") {
		t.Fatal("API-only metrics exposed worker operational gauges")
	}
}

func assertMetric(t *testing.T, metrics *Metrics, want string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), want+"\n") {
		t.Fatalf("metrics output does not contain %q", want)
	}
}

func assertMetricPresent(t *testing.T, metrics *Metrics, want string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), want) {
		t.Fatalf("metrics output does not contain %q", want)
	}
}
