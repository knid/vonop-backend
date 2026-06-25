package controllers

import "testing"

func TestParseProviderModelSupportsReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning, providerPin := c.parseProviderModel("qwen/qwen3.7-plus;medium")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning == nil || reasoning.Effort != "medium" {
		t.Fatalf("reasoning = %#v, want effort medium", reasoning)
	}
	if providerPin != "" {
		t.Fatalf("providerPin = %q, want empty", providerPin)
	}
}

func TestParseProviderModelSupportsOpenRouterProviderPinSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning, providerPin := c.parseProviderModel("z-ai/glm-5.2;medium;gmicloud/fp8")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "z-ai/glm-5.2" {
		t.Fatalf("model = %q, want z-ai/glm-5.2", model)
	}
	if reasoning == nil || reasoning.Effort != "medium" {
		t.Fatalf("reasoning = %#v, want effort medium", reasoning)
	}
	if providerPin != "gmicloud/fp8" {
		t.Fatalf("providerPin = %q, want gmicloud/fp8", providerPin)
	}
}

func TestParseProviderModelSupportsExplicitProviderWithReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning, providerPin := c.parseProviderModel("openrouter/qwen/qwen3.7-plus;minimal")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning == nil || reasoning.Effort != "minimal" {
		t.Fatalf("reasoning = %#v, want effort minimal", reasoning)
	}
	if providerPin != "" {
		t.Fatalf("providerPin = %q, want empty", providerPin)
	}
}

func TestParseProviderModelIgnoresInvalidReasoningEffortSuffix(t *testing.T) {
	c := &Controller{}

	provider, model, reasoning, providerPin := c.parseProviderModel("qwen/qwen3.7-plus;turbo")

	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "qwen/qwen3.7-plus" {
		t.Fatalf("model = %q, want qwen/qwen3.7-plus", model)
	}
	if reasoning != nil {
		t.Fatalf("reasoning = %#v, want nil", reasoning)
	}
	if providerPin != "" {
		t.Fatalf("providerPin = %q, want empty", providerPin)
	}
}
