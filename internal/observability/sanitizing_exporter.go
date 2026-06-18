package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var blockedLangfuseExportAttributeKeys = map[attribute.Key]struct{}{
	"db.user":              {},
	"db.name":              {},
	"db.connection_string": {},
	"server.address":       {},
	"server.port":          {},
	"net.peer.name":        {},
	"net.peer.port":        {},
	"network.peer.address": {},
	"network.peer.port":    {},
}

type sanitizingSpanExporter struct {
	next sdktrace.SpanExporter
}

type sanitizedReadOnlySpan struct {
	sdktrace.ReadOnlySpan
}

// NewSanitizingSpanExporter strips sensitive infrastructure attributes before
// spans leave the process. It intentionally preserves all Langfuse-specific
// trace and observation attributes.
func NewSanitizingSpanExporter(next sdktrace.SpanExporter) sdktrace.SpanExporter {
	if next == nil {
		return nil
	}
	return sanitizingSpanExporter{next: next}
}

func (e sanitizingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if len(spans) == 0 {
		return e.next.ExportSpans(ctx, spans)
	}
	sanitized := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		sanitized[i] = sanitizedReadOnlySpan{ReadOnlySpan: span}
	}
	return e.next.ExportSpans(ctx, sanitized)
}

func (e sanitizingSpanExporter) Shutdown(ctx context.Context) error {
	return e.next.Shutdown(ctx)
}

func (s sanitizedReadOnlySpan) Attributes() []attribute.KeyValue {
	attrs := s.ReadOnlySpan.Attributes()
	if len(attrs) == 0 {
		return attrs
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if _, blocked := blockedLangfuseExportAttributeKeys[attr.Key]; blocked {
			continue
		}
		out = append(out, attr)
	}
	return out
}
