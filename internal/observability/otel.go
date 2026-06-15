package observability

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/knid/wemob"

func InitLangfuse(ctx context.Context) (func(context.Context) error, error) {
	return InitLangfuseWithSecrets(ctx, nil)
}

func InitLangfuseWithSecrets(ctx context.Context, secrets SecretProvider) (func(context.Context) error, error) {
	cfg := LangfuseConfigFromEnvAndSecrets(secrets)
	if !cfg.Enabled {
		slog.Warn("Langfuse OpenTelemetry exporter disabled; LANGFUSE_PUBLIC_KEY or LANGFUSE_SECRET_KEY is not configured")
		return func(context.Context) error { return nil }, nil
	}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("Langfuse OpenTelemetry exporter error", "error", err)
	}))

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.Endpoint),
		otlptracehttp.WithHeaders(cfg.Headers),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
		otlptracehttp.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("",
			// Use raw attribute keys to avoid another dependency surface.
			// These are standard OpenTelemetry resource attributes.
			attribute.String("service.name", "wemob-backend"),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	slog.Info("Langfuse OpenTelemetry exporter enabled", "endpoint", cfg.Endpoint, "environment", cfg.Environment)

	return provider.Shutdown, nil
}

func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

func StartTrace(ctx context.Context, in TraceAttributeInput) (context.Context, trace.Span) {
	return Tracer().Start(ctx, in.Name, trace.WithAttributes(TraceAttributes(in)...))
}

func StartGeneration(ctx context.Context, name string, in GenerationAttributeInput, round int, startedAt time.Time) (context.Context, trace.Span) {
	if name == "" {
		name = "llm-generation"
	}
	opts := []trace.SpanStartOption{trace.WithAttributes(GenerationAttributes(in, round)...)}
	if !startedAt.IsZero() {
		opts = append(opts, trace.WithTimestamp(startedAt))
	}
	return Tracer().Start(ctx, name, opts...)
}

func StartTool(ctx context.Context, toolName, input string, isError bool, durationMs int, startedAt time.Time) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithAttributes(ToolAttributes(toolName, input, isError, durationMs)...)}
	if !startedAt.IsZero() {
		opts = append(opts, trace.WithTimestamp(startedAt))
	}
	ctx, span := Tracer().Start(ctx, "tool."+toolName, opts...)
	if isError {
		span.SetStatus(codes.Error, "tool execution failed")
	}
	return ctx, span
}

func RecordTool(ctx context.Context, toolName, input string, isError bool, durationMs int, startedAt time.Time, extraAttrs ...attribute.KeyValue) {
	_, span := StartTool(ctx, toolName, input, isError, durationMs, startedAt)
	if len(extraAttrs) > 0 {
		span.SetAttributes(extraAttrs...)
	}
	endOpts := []trace.SpanEndOption{}
	if !startedAt.IsZero() {
		endOpts = append(endOpts, trace.WithTimestamp(startedAt.Add(time.Duration(durationMs)*time.Millisecond)))
	}
	span.End(endOpts...)
}
