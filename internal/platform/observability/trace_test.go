package observability

import (
	"context"
	"testing"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIncidentTraceUsesTheIncidentIDAsTraceID(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithIDGenerator(idGenerator{}), tracesdk.WithSpanProcessor(recorder))
	tracer := provider.Tracer("test")

	ctx := WithIncidentTrace(context.Background(), "60a371f0-acb3-4c8e-95d2-f1143edd5ab8")
	ctx, root := tracer.Start(ctx, "step")
	_, child := tracer.Start(ctx, "http")
	child.End()
	root.End()
	_, other := tracer.Start(context.Background(), "unrelated")
	other.End()

	spans := recorder.Ended()
	if got := spans[1].SpanContext().TraceID().String(); got != "60a371f0acb34c8e95d2f1143edd5ab8" {
		t.Fatalf("root trace ID = %s, want the incident ID", got)
	}
	if spans[0].SpanContext().TraceID() != spans[1].SpanContext().TraceID() {
		t.Fatal("child left the incident's trace")
	}
	if spans[1].SpanContext().SpanID() == spans[0].SpanContext().SpanID() || !spans[0].SpanContext().SpanID().IsValid() {
		t.Fatal("span IDs must be random and distinct")
	}
	if spans[2].SpanContext().TraceID() == spans[1].SpanContext().TraceID() || !spans[2].SpanContext().TraceID().IsValid() {
		t.Fatal("spans outside an incident must get a random trace ID")
	}
	if WithIncidentTrace(context.Background(), "not-a-uuid") != context.Background() {
		t.Fatal("a non-UUID must leave the context unchanged")
	}
}
