package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordFromPythonUsage_AppendsEntryWithUpstreamCost(t *testing.T) {
	tracker := NewTokenTracker()
	tracker.RecordFromPythonUsage(PythonUsageEntry{
		SpanID:           "span-1",
		Provider:         "openrouter",
		Model:            "anthropic/claude-sonnet-4.6",
		PromptTokens:     100,
		CompletionTokens: 50,
		CacheReadTokens:  10,
		CacheWriteTokens: 5,
		CostUSD:          0.0023,
		LatencyMs:        200,
		Subagent:         "design_direction_agent",
		RuntimeKind:      "deepagents",
	})

	entries := tracker.Entries()
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "anthropic/claude-sonnet-4.6", e.Model)
	assert.Equal(t, 100, e.PromptTokens)
	assert.Equal(t, 50, e.CompletionTokens)
	assert.Equal(t, 10, e.CacheReadInputTokens)
	assert.Equal(t, 5, e.CacheCreationInputTokens)
	assert.Equal(t, 0.0023, e.CostUSD)
	assert.Equal(t, int64(200), e.LatencyMs)
	assert.Equal(t, "design_direction_agent", e.Subagent)
	assert.Equal(t, "span-1", e.SpanID)
	assert.Equal(t, "upstream", e.CostSource)
}

func TestRecordFromPythonUsage_FallsBackToCatalogWhenCostZero(t *testing.T) {
	tracker := NewTokenTracker()
	tracker.RecordFromPythonUsage(PythonUsageEntry{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1_000_000,
		CompletionTokens: 0,
		// CostUSD=0 → catalog estimator should kick in
	})
	entries := tracker.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "catalog_estimate", entries[0].CostSource)
	assert.Greater(t, entries[0].CostUSD, 0.0, "catalog estimator should produce a positive cost")
}

func TestRecordFromPythonUsage_TotalsAggregateAcrossEntries(t *testing.T) {
	tracker := NewTokenTracker()
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 100, CompletionTokens: 50, Subagent: "main"})
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 200, CompletionTokens: 100, Subagent: "frontend_builder_agent"})
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 300, CompletionTokens: 150, Subagent: "qa_debug_agent"})

	prompt, completion, total, callCount := tracker.Totals()
	assert.Equal(t, 600, prompt)
	assert.Equal(t, 300, completion)
	assert.Equal(t, 900, total)
	assert.Equal(t, 3, callCount)
}

func TestRecordFromPythonUsage_ToolsUsedSurfacesSubagents(t *testing.T) {
	tracker := NewTokenTracker()
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 1, CompletionTokens: 1, Subagent: "design_direction_agent"})
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 1, CompletionTokens: 1, Subagent: "frontend_builder_agent"})
	tracker.RecordFromPythonUsage(PythonUsageEntry{Model: "m", PromptTokens: 1, CompletionTokens: 1, Subagent: "design_direction_agent"})

	used := tracker.ToolsUsed()
	assert.Contains(t, used, "subagent:design_direction_agent")
	assert.Contains(t, used, "subagent:frontend_builder_agent")
	// Duplicate subagent names are deduplicated.
	assert.Equal(t, len(used), 2)
}

func TestRecordFromPythonUsage_NoUsageMeansZeroCredits(t *testing.T) {
	tracker := NewTokenTracker()
	// No RecordFromPythonUsage calls — entries is empty.
	prompt, completion, total, callCount := tracker.Totals()
	assert.Equal(t, 0, prompt)
	assert.Equal(t, 0, completion)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, callCount)
}

func TestRecordFromPythonUsage_InterleavesWithRecord(t *testing.T) {
	// The native Go runtime uses Record(); the deepagents runtime uses
	// RecordFromPythonUsage. Both must land in the same entries slice so
	// finaliseSuccess/finaliseFailureV2 see a unified total.
	tracker := NewTokenTracker()
	tracker.Record("anthropic/claude-sonnet-4-6", UsageInfo{
		PromptTokens:     50,
		CompletionTokens: 25,
		TotalTokens:      75,
	}, 100, false, "stream_text")
	tracker.RecordFromPythonUsage(PythonUsageEntry{
		Model:            "anthropic/claude-sonnet-4-6",
		PromptTokens:     80,
		CompletionTokens: 40,
	})

	prompt, completion, _, callCount := tracker.Totals()
	assert.Equal(t, 130, prompt)
	assert.Equal(t, 65, completion)
	assert.Equal(t, 2, callCount)
}
