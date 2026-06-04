package agent

import (
	"slices"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestCompileAgentContext_WebSEO(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	packet := CompileAgentContext(AgentContextInput{
		Project: project,
		Message: "/seo",
	})

	if packet.Profile.ID != AgentIDWebBuilder {
		t.Fatalf("agent = %q, want %q", packet.Profile.ID, AgentIDWebBuilder)
	}
	if !slices.Contains(packet.SkillIDs, "web-seo") {
		t.Fatalf("expected /seo to activate web-seo, got %v", packet.SkillIDs)
	}
	if packet.ReadOnly {
		t.Fatal("/seo should not be read-only")
	}
	if len(packet.SystemMessages) == 0 {
		t.Fatal("expected system messages")
	}
}

func TestCompileAgentContext_ReadOnlyCommand(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	packet := CompileAgentContext(AgentContextInput{
		Project: project,
		Message: "/review",
	})

	if !packet.ReadOnly {
		t.Fatal("/review should compile as read-only")
	}
}
