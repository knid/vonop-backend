package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/constants"
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

func TestGatewayCatalogIncludesRequestedBuilderModels(t *testing.T) {
	glm := constants.GatewayChatModelByID("z-ai/glm-5.2")
	if glm == nil {
		t.Fatal("expected z-ai/glm-5.2 in chat catalog")
	}
	if slices.Contains(glm.Capabilities, "vision") {
		t.Fatalf("z-ai/glm-5.2 should be text-only, got capabilities %#v", glm.Capabilities)
	}

	for _, id := range []string{"moonshotai/kimi-k2.7-code", "kimi-k2.6"} {
		model := constants.GatewayChatModelByID(id)
		if model == nil {
			t.Fatalf("expected %s in chat catalog", id)
		}
		if !slices.Contains(model.Capabilities, "vision") {
			t.Fatalf("%s should support vision, got capabilities %#v", id, model.Capabilities)
		}
	}
}
