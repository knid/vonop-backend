package controllers

import "testing"

func TestParseProviderModelSupportsReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning := c.parseProviderModel("qwen/qwen3.7-plus;medium")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning == nil || reasoning.Effort != "medium" {
		t.Fatalf("reasoning = %#v, want effort medium", reasoning)
	}
}

func TestParseProviderModelSupportsExplicitProviderWithReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning := c.parseProviderModel("openrouter/qwen/qwen3.7-plus;minimal")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning == nil || reasoning.Effort != "minimal" {
		t.Fatalf("reasoning = %#v, want effort minimal", reasoning)
	}
}

func TestParseProviderModelIgnoresInvalidReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning := c.parseProviderModel("qwen/qwen3.7-plus;turbo")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning != nil {
		t.Fatalf("reasoning = %#v, want nil", reasoning)
	}
}
