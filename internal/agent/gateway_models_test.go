package agent

import (
	"strings"
	"testing"
)

func TestGatewayAllowedModelsByTypeCanonicalizesAliases(t *testing.T) {
	deps := &RunDependencies{
		GatewayAllowedModels: func() []string {
			return []string{
				"gpt-4.1-mini",
				"claude-haiku-4-5",
				"flux/schnell",
				"not-a-real-model",
			}
		},
	}

	chat := gatewayAllowedChatModels(deps)
	if len(chat) != 2 {
		t.Fatalf("expected 2 chat models, got %d: %#v", len(chat), chat)
	}
	if chat[0].ID != "openai/gpt-4.1-mini" || chat[1].ID != "anthropic/claude-haiku-4-5" {
		t.Fatalf("unexpected chat models: %#v", chat)
	}

	image := gatewayAllowedImageModels(deps)
	if len(image) != 1 || image[0].ID != "fal-ai/flux/schnell" {
		t.Fatalf("unexpected image models: %#v", image)
	}
}

func TestRenderGatewayImageModelsBlock(t *testing.T) {
	if got := renderGatewayImageModelsBlock(nil); !strings.Contains(got, "none on the user's current plan") {
		t.Fatalf("empty image model block = %q", got)
	}

	deps := &RunDependencies{GatewayAllowedModels: func() []string { return []string{"fal-ai/nano-banana-2"} }}
	got := renderGatewayImageModelsBlock(gatewayAllowedImageModels(deps))
	if !strings.Contains(got, "fal-ai/nano-banana-2") || !strings.Contains(got, "(default)") {
		t.Fatalf("image model block missing expected content: %q", got)
	}
}
