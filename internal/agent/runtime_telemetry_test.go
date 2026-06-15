package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimePhaseSpanNames(t *testing.T) {
	assert.Equal(t, "initialize-run", RuntimePhaseInitializeRun.SpanName())
	assert.Equal(t, "load-project-state", RuntimePhaseLoadProjectState.SpanName())
	assert.Equal(t, "compile-runtime-context", RuntimePhaseCompileRuntimeContext.SpanName())
	assert.Equal(t, "build-runtime-tools", RuntimePhaseBuildRuntimeTools.SpanName())
	assert.Equal(t, "execute-agent-loop", RuntimePhaseExecuteAgentLoop.SpanName())
	assert.Equal(t, "persist-assistant-turn", RuntimePhasePersistAssistantTurn.SpanName())
	assert.Equal(t, "finalize-run", RuntimePhaseFinalizeRun.SpanName())
	assert.Equal(t, "record-usage-and-scores", RuntimePhaseRecordUsageAndScores.SpanName())
}

func TestRuntimePhaseStringFallsBackToRawValue(t *testing.T) {
	phase := RuntimePhase("custom-phase")
	assert.Equal(t, "custom-phase", phase.SpanName())
}
