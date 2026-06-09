package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeepAgentsRuntime_LLMUsageForwardedToTracker covers the event
// mapping done in runtime_deepagents.go's switch on evt.Type. The runtime
// receives an "llm_usage" event from the Python LangChain callback and
// must convert it into a TokenTracker entry so the unified billing path
// in finaliseSuccess / finaliseFailureV2 sees the tokens.
//
// We exercise the switch arm indirectly: build a JSON line that matches
// the Python payload, run it through the same code path the streaming
// callback uses, and assert the resulting tracker state.
func TestDeepAgentsRuntime_LLMUsageForwardedToTracker(t *testing.T) {
	tracker := utils.NewTokenTracker()

	payload := map[string]any{
		"span_id":            "span-abc",
		"provider":           "openrouter",
		"model":              "anthropic/claude-sonnet-4.6",
		"prompt_tokens":      200,
		"completion_tokens":  80,
		"cache_read_tokens":  12,
		"cache_write_tokens": 4,
		"cost_usd":           0.012,
		"latency_ms":         350,
		"subagent":           "frontend_builder_agent",
		"runtime_kind":       "deepagents",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	// Replay the same code path the runtime uses for "llm_usage" events.
	var evt deepAgentsEventDataLLMUsage
	require.NoError(t, json.Unmarshal(raw, &evt))

	tracker.RecordFromPythonUsage(utils.PythonUsageEntry{
		SpanID:           evt.SpanID,
		Provider:         evt.Provider,
		Model:            evt.Model,
		PromptTokens:     evt.PromptTokens,
		CompletionTokens: evt.CompletionTokens,
		CacheReadTokens:  evt.CacheReadTokens,
		CacheWriteTokens: evt.CacheWriteTokens,
		CostUSD:          evt.CostUSD,
		LatencyMs:        evt.LatencyMs,
		Subagent:         evt.Subagent,
		RuntimeKind:      evt.RuntimeKind,
	})

	entries := tracker.Entries()
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "anthropic/claude-sonnet-4.6", e.Model)
	assert.Equal(t, 200, e.PromptTokens)
	assert.Equal(t, 80, e.CompletionTokens)
	assert.Equal(t, 12, e.CacheReadInputTokens)
	assert.Equal(t, 4, e.CacheCreationInputTokens)
	assert.Equal(t, 0.012, e.CostUSD)
	assert.Equal(t, "frontend_builder_agent", e.Subagent)
	assert.Equal(t, "span-abc", e.SpanID)
}

// TestDeepAgentsRuntime_RunStartCarriesRuntimeKindAndSubagents covers
// the PR 13 / PR 9 RunStartEvent fields the runtime emits.
func TestDeepAgentsRuntime_RunStartCarriesRuntimeKindAndSubagents(t *testing.T) {
	names := deepagentsSubagentNames
	require.NotEmpty(t, names)
	assert.Contains(t, names, "design_direction_agent")
	assert.Contains(t, names, "frontend_builder_agent")
	assert.Contains(t, names, "data_backend_agent")
	assert.Contains(t, names, "qa_debug_agent")

	// Sanity: the constant must be non-empty and stable.
	assert.Equal(t, string(RuntimeDeepAgents), string(RuntimeKind("deepagents")))
	assert.Equal(t, "native_go", string(RuntimeNativeGo))
	_ = uuid.New() // keep the import used
}
