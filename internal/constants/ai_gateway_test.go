package constants

import "testing"

func TestGatewayCanonicalModelID(t *testing.T) {
	tests := map[string]string{
		"gpt-4.1-mini":           "openai/gpt-4.1-mini",
		" claude-haiku-4-5 ":     "anthropic/claude-haiku-4-5",
		"gemini-2.5-pro":         "google/gemini-2.5-pro",
		"flux-pro/v1.1-ultra":    "fal-ai/flux-pro/v1.1-ultra",
		"stepfun/step-3.7-flash": "stepfun/step-3.7-flash",
		"unknown/model":          "unknown/model",
	}
	for input, want := range tests {
		if got := GatewayCanonicalModelID(input); got != want {
			t.Fatalf("GatewayCanonicalModelID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGatewayModelByIDAcceptsAliases(t *testing.T) {
	if got := GatewayModelByID("claude-haiku-4-5"); got == nil || got.ID != "anthropic/claude-haiku-4-5" {
		t.Fatalf("GatewayModelByID alias failed, got %#v", got)
	}
	if got := GatewayChatModelByID("flux/schnell"); got != nil {
		t.Fatalf("GatewayChatModelByID should reject image model alias, got %#v", got)
	}
}
