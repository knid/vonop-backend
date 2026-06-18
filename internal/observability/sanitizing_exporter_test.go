package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type captureSpanExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *captureSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *captureSpanExporter) Shutdown(context.Context) error {
	return nil
}

func TestSanitizingSpanExporterDropsSensitiveInfrastructureAttributes(t *testing.T) {
	capture := &captureSpanExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewSanitizingSpanExporter(capture)))
	tracer := provider.Tracer("test")

	_, span := tracer.Start(context.Background(), "compile-runtime-context")
	span.SetAttributes(
		attribute.String("db.user", "secret-user"),
		attribute.String("db.name", "prod-db"),
		attribute.String("db.connection_string", "postgres://secret"),
		attribute.String("server.address", "db.internal:5432"),
		attribute.Int("server.port", 5432),
		attribute.String("net.peer.name", "db.internal"),
		attribute.Int("net.peer.port", 5432),
		attribute.String("network.peer.address", "10.0.0.1"),
		attribute.Int("network.peer.port", 5432),
		attribute.String("langfuse.trace.name", "app_chat"),
		attribute.String("langfuse.observation.type", "span"),
	)
	span.End()

	if len(capture.spans) != 1 {
		t.Fatalf("captured spans = %d, want 1", len(capture.spans))
	}
	attrs := attrsByKey(capture.spans[0].Attributes())
	for _, key := range []string{
		"db.user",
		"db.name",
		"db.connection_string",
		"server.address",
		"server.port",
		"net.peer.name",
		"net.peer.port",
		"network.peer.address",
		"network.peer.port",
	} {
		if _, ok := attrs[key]; ok {
			t.Fatalf("sensitive attr %q was exported", key)
		}
	}
	assertStringAttr(t, attrs, "langfuse.trace.name", "app_chat")
	assertStringAttr(t, attrs, "langfuse.observation.type", "span")
}
