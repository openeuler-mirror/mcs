package shim_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TODO:
// Hook the test in CI
// Write the tracing helper into the actual shim interceptors/TRACEPARENT

type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string {
	return c[key]
}

func (c mapCarrier) Set(key, value string) {
	c[key] = value
}

func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

func TestTracePropagationInjectExtractCreatesSameTrace(t *testing.T) {
	t.Parallel()

	tp := sdktrace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()
	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(propagation.TraceContext{})
	tracer := otel.Tracer("shim-test-tracer")

	ctxParent, parent := tracer.Start(context.Background(), "containerd-parent")
	defer parent.End()

	carrier := mapCarrier{}
	otel.GetTextMapPropagator().Inject(ctxParent, carrier)

	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	ctxChild, child := tracer.Start(extractedCtx, "shim-child")
	defer child.End()

	parentSpan := trace.SpanFromContext(ctxParent)
	childSpan := trace.SpanFromContext(ctxChild)

	if !parentSpan.SpanContext().IsValid() || !childSpan.SpanContext().IsValid() {
		t.Fatal("expected both span contexts to be valid")
	}

	if parentSpan.SpanContext().TraceID() != childSpan.SpanContext().TraceID() {
		t.Fatalf(
			"trace IDs should match: parent=%s child=%s",
			parentSpan.SpanContext().TraceID(),
			childSpan.SpanContext().TraceID(),
		)
	}

	var zeroSpanID trace.SpanID
	if childSpan.SpanContext().SpanID() == zeroSpanID {
		t.Fatal("child span should have a non-zero span ID")
	}

	if parentSpan.SpanContext().SpanID() == zeroSpanID {
		t.Fatal("parent span should have a non-zero span ID")
	}
}
