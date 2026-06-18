package agent

import (
	"context"

	"github.com/knid/wemob/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type RuntimePhase string

const (
	RuntimePhaseInitializeRun         RuntimePhase = "initialize-run"
	RuntimePhaseLoadProjectState      RuntimePhase = "load-project-state"
	RuntimePhaseCompileRuntimeContext RuntimePhase = "compile-runtime-context"
	RuntimePhaseBuildRuntimeTools     RuntimePhase = "build-runtime-tools"
	RuntimePhaseExecuteAgentLoop      RuntimePhase = "execute-agent-loop"
	RuntimePhasePersistUserTurn       RuntimePhase = "persist-user-turn"
	RuntimePhaseFinalizeRun           RuntimePhase = "finalize-run"
	RuntimePhaseRecordUsageAndScores  RuntimePhase = "record-usage-and-scores"
)

func (p RuntimePhase) SpanName() string {
	return string(p)
}

func startRuntimePhase(ctx context.Context, phase RuntimePhase, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	baseAttrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "span"),
		attribute.String("langfuse.observation.metadata.runtime_phase", phase.SpanName()),
	}
	baseAttrs = append(baseAttrs, attrs...)
	return observability.Tracer().Start(ctx, phase.SpanName(), trace.WithAttributes(baseAttrs...))
}

func withRuntimePhase(ctx context.Context, phase RuntimePhase, fn func(context.Context) error) error {
	phaseCtx, span := startRuntimePhase(ctx, phase)
	defer span.End()
	return fn(phaseCtx)
}
