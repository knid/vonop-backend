package agent

import (
	"context"
	"testing"

	"github.com/knid/wemob/internal/observability"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRuntimePhaseSpanNames(t *testing.T) {
	assert.Equal(t, "initialize-run", RuntimePhaseInitializeRun.SpanName())
	assert.Equal(t, "load-project-state", RuntimePhaseLoadProjectState.SpanName())
	assert.Equal(t, "compile-runtime-context", RuntimePhaseCompileRuntimeContext.SpanName())
	assert.Equal(t, "build-runtime-tools", RuntimePhaseBuildRuntimeTools.SpanName())
	assert.Equal(t, "execute-agent-loop", RuntimePhaseExecuteAgentLoop.SpanName())
	assert.Equal(t, "persist-user-turn", RuntimePhasePersistUserTurn.SpanName())
	assert.Equal(t, "finalize-run", RuntimePhaseFinalizeRun.SpanName())
	assert.Equal(t, "record-usage-and-scores", RuntimePhaseRecordUsageAndScores.SpanName())
}

func TestRuntimePhaseStringFallsBackToRawValue(t *testing.T) {
	phase := RuntimePhase("custom-phase")
	assert.Equal(t, "custom-phase", phase.SpanName())
}

func TestWithRuntimePhaseReturnsFunctionResult(t *testing.T) {
	called := false
	err := withRuntimePhase(t.Context(), RuntimePhaseLoadProjectState, func(ctx context.Context) error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestRuntimePhasesShareRootParentWhenContextIsNotMutated(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
	})

	ctx, root := observability.Tracer().Start(t.Context(), "app_chat")
	err := withRuntimePhase(ctx, RuntimePhaseLoadProjectState, func(context.Context) error {
		return nil
	})
	assert.NoError(t, err)
	err = withRuntimePhase(ctx, RuntimePhaseCompileRuntimeContext, func(context.Context) error {
		return nil
	})
	assert.NoError(t, err)
	root.End()

	var loadParent, compileParent string
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case RuntimePhaseLoadProjectState.SpanName():
			loadParent = span.Parent().SpanID().String()
		case RuntimePhaseCompileRuntimeContext.SpanName():
			compileParent = span.Parent().SpanID().String()
		}
	}

	assert.NotEmpty(t, loadParent)
	assert.Equal(t, loadParent, compileParent)
	assert.Equal(t, root.SpanContext().SpanID().String(), loadParent)
}
