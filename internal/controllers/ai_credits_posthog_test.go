package controllers

import (
	"math"
	"testing"

	"github.com/knid/wemob/internal/utils"
)

func TestPostHogCostSplitUsesCatalogEstimate(t *testing.T) {
	entry := utils.TokenEntry{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 1000,
		CostUSD:          0.018,
		CostSource:       "catalog_estimate",
	}

	input, output, source := postHogCostSplit(entry)

	if source != "catalog_estimate" {
		t.Fatalf("source = %q, want catalog_estimate", source)
	}
	if !almostEqual(input, 0.003) {
		t.Fatalf("input = %f, want 0.003", input)
	}
	if !almostEqual(output, 0.015) {
		t.Fatalf("output = %f, want 0.015", output)
	}
}

func TestPostHogCostSplitAllocatesUpstreamTotal(t *testing.T) {
	entry := utils.TokenEntry{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 1000,
		CostUSD:          0.036,
		CostSource:       "upstream",
	}

	input, output, source := postHogCostSplit(entry)

	if source != "allocated_from_upstream_total" {
		t.Fatalf("source = %q, want allocated_from_upstream_total", source)
	}
	if !almostEqual(input+output, entry.CostUSD) {
		t.Fatalf("split total = %f, want %f", input+output, entry.CostUSD)
	}
	if !almostEqual(input, 0.006) {
		t.Fatalf("input = %f, want 0.006", input)
	}
	if !almostEqual(output, 0.030) {
		t.Fatalf("output = %f, want 0.030", output)
	}
}

func TestToolCallNamesDedupesFunctionNames(t *testing.T) {
	names := toolCallNames([]utils.ToolCall{
		{Function: &utils.FunctionCall{Name: "read_files"}},
		{Function: &utils.FunctionCall{Name: "read_files"}},
		{Function: &utils.FunctionCall{Name: "generate_image"}},
		{},
	})

	if len(names) != 2 || names[0] != "read_files" || names[1] != "generate_image" {
		t.Fatalf("names = %#v", names)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.000000001
}
