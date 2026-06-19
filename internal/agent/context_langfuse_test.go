package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
)

func TestCompileAgentContextUsesLangfusePromptResolver(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeApp}
	var names []string
	deps := &RunDependencies{
		ResolveLangfusePrompt: func(ctx context.Context, name, fallback string, vars map[string]string) (observability.CompiledPrompt, error) {
			names = append(names, name)
			return observability.CompiledPrompt{
				Name:    name,
				Version: 3,
				Text:    "remote:" + name,
			}, nil
		},
	}

	packet := CompileAgentContext(AgentContextInput{
		Context:    context.Background(),
		Project:    project,
		Deps:       deps,
		IsFirstGen: true,
	})

	if len(packet.SystemMessages) < 3 {
		t.Fatalf("expected system messages, got %d", len(packet.SystemMessages))
	}
	if packet.SystemMessages[0].Content != "remote:wemob-agent-web-base" {
		t.Fatalf("base prompt = %q", packet.SystemMessages[0].Content)
	}
	if packet.SystemMessages[1].Content != "remote:wemob-agent-mode-first-gen" {
		t.Fatalf("mode prompt = %q", packet.SystemMessages[1].Content)
	}
	if len(packet.PromptParts) < 2 {
		t.Fatalf("expected prompt parts, got %+v", packet.PromptParts)
	}
	if packet.PromptParts[0] != (observability.PromptLink{Name: "wemob-agent-web-base", Version: 3}) {
		t.Fatalf("first prompt part = %+v", packet.PromptParts[0])
	}
	if len(names) == 0 {
		t.Fatal("resolver was not called")
	}
}

func TestCompileAgentContextFallsBackWhenLangfusePromptResolverFails(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), ProjectType: models.ProjectTypeApp}
	deps := &RunDependencies{
		ResolveLangfusePrompt: func(ctx context.Context, name, fallback string, vars map[string]string) (observability.CompiledPrompt, error) {
			return observability.CompiledPrompt{}, errors.New("langfuse unavailable")
		},
	}

	packet := CompileAgentContext(AgentContextInput{
		Context: context.Background(),
		Project: project,
		Deps:    deps,
	})

	if packet.SystemMessages[0].Content != CompactBasePromptWeb {
		t.Fatal("expected local base prompt fallback")
	}
	if packet.PromptParts[0].Version != 0 {
		t.Fatalf("fallback prompt version = %d", packet.PromptParts[0].Version)
	}
}
