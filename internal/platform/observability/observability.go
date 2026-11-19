package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Metrics struct {
	EventsAccepted                     prometheus.Counter
	EventsDuplicate                    prometheus.Counter
	StepsCompleted                     *prometheus.CounterVec
	StepDuration                       *prometheus.HistogramVec
	PipelineChecks                     *prometheus.CounterVec
	RunnableSteps                      prometheus.Gauge
	OldestRunnableStepAgeSeconds       prometheus.Gauge
	ExpiredStepLeases                  prometheus.Gauge
	PublishableOutboxMessages          prometheus.Gauge
	OldestPublishableOutboxAgeSeconds  prometheus.Gauge
	ExhaustedUnpublishedOutboxMessages prometheus.Gauge
	OperationalMetricsLastSuccess      prometheus.Gauge
	registry                           *prometheus.Registry
}

func NewMetrics(enableOperational bool) *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		EventsAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "nimbus", Name: "events_accepted_total", Help: "Accepted events.",
		}),
		EventsDuplicate: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "nimbus", Name: "events_duplicate_total", Help: "Idempotent duplicate events.",
		}),
		StepsCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "nimbus", Name: "steps_completed_total", Help: "Completed step attempts by outcome.",
		}, []string{"type", "outcome"}),
		StepDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "nimbus", Name: "step_duration_seconds", Help: "Step execution duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"type"}),
		PipelineChecks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "nimbus", Name: "pipeline_checks_total", Help: "Scheduled CloudWatch pipeline checks by outcome.",
		}, []string{"outcome"}),
		RunnableSteps: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "runnable_steps", Help: "PostgreSQL-authoritative steps runnable now, including reclaimable expired leases.",
		}),
		OldestRunnableStepAgeSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "oldest_runnable_step_age_seconds", Help: "Non-negative age of the oldest step runnable now.",
		}),
		ExpiredStepLeases: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "expired_step_leases", Help: "PostgreSQL-authoritative running step leases past their expiry.",
		}),
		PublishableOutboxMessages: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "publishable_outbox_messages", Help: "PostgreSQL-authoritative unpublished outbox messages publishable now.",
		}),
		OldestPublishableOutboxAgeSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "oldest_publishable_outbox_age_seconds", Help: "Non-negative age of the oldest outbox message publishable now.",
		}),
		ExhaustedUnpublishedOutboxMessages: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "exhausted_unpublished_outbox_messages", Help: "PostgreSQL-authoritative unpublished outbox messages with no attempts remaining.",
		}),
		OperationalMetricsLastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "nimbus", Name: "operational_metrics_last_success_timestamp_seconds", Help: "Unix timestamp of the last successful PostgreSQL operational metrics refresh.",
		}),
		registry: registry,
	}
	registry.MustRegister(metrics.EventsAccepted, metrics.EventsDuplicate, metrics.StepsCompleted, metrics.StepDuration, metrics.PipelineChecks)
	if enableOperational {
		registry.MustRegister(metrics.RunnableSteps, metrics.OldestRunnableStepAgeSeconds, metrics.ExpiredStepLeases,
			metrics.PublishableOutboxMessages, metrics.OldestPublishableOutboxAgeSeconds,
			metrics.ExhaustedUnpublishedOutboxMessages, metrics.OperationalMetricsLastSuccess)
	}
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func SetupTracing(ctx context.Context, endpoint string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	serviceResource, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", "nimbus"),
		attribute.String("service.version", "0.1.0"),
	))
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}
	provider := tracesdk.NewTracerProvider(
		tracesdk.WithIDGenerator(idGenerator{}),
		tracesdk.WithBatcher(exporter),
		tracesdk.WithResource(serviceResource),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

type traceIDKey struct{}

// WithIncidentTrace makes root spans started from the returned context use the incident's UUID as
// their trace ID. Every product that handles the incident propagates W3C traceparent, so everything
// done about one incident (kill switch, notifications, restore, escalation) lands in one trace whose
// ID anyone can derive from the incident ID. Returns ctx unchanged if id is not a UUID.
func WithIncidentTrace(ctx context.Context, id string) context.Context {
	raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(raw) != 16 {
		return ctx
	}
	var traceID trace.TraceID
	copy(traceID[:], raw)
	if !traceID.IsValid() {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// idGenerator is random like the SDK default, except for the trace ID pinned by WithIncidentTrace.
type idGenerator struct{}

func (idGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	traceID, ok := ctx.Value(traceIDKey{}).(trace.TraceID)
	if !ok {
		_, _ = rand.Read(traceID[:])
	}
	return traceID, idGenerator{}.NewSpanID(ctx, traceID)
}

func (idGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	var spanID trace.SpanID
	_, _ = rand.Read(spanID[:])
	return spanID
}
