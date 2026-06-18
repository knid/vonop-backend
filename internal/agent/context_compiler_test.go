package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestCompileAgentContext_WebSEO(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	packet := CompileAgentContext(AgentContextInput{
		Project:    project,
		Message:    "/seo",
		IsFirstGen: false,
	})

	if packet.Profile.ID != AgentIDWebBuilder {
		t.Fatalf("agent = %q, want %q", packet.Profile.ID, AgentIDWebBuilder)
	}
	if slices.Contains(packet.SkillIDs, "web-seo") {
		t.Fatalf("/seo should not inject full web-seo by default, got active %v", packet.SkillIDs)
	}
	if !slices.Contains(packet.SuggestedSkillIDs, "web-seo") {
		t.Fatalf("expected /seo to suggest web-seo, got %v", packet.SuggestedSkillIDs)
	}
	if packet.ReadOnly {
		t.Fatal("/seo should not be read-only")
	}
	if len(packet.SystemMessages) == 0 {
		t.Fatal("expected system messages")
	}
	combined := ""
	for _, msg := range packet.SystemMessages {
		combined += msg.Content + "\n"
	}
	if strings.Contains(combined, "AVAILABLE SKILLS") {
		t.Fatalf("existing edit context should not inject the skill manifest:\n%s", combined)
	}
	if strings.Contains(combined, "# SKILL: Web SEO") {
		t.Fatal("optional web-seo skill body should not be injected as a system message")
	}
}

func TestCompileAgentContext_WebEditDoesNotInjectRequiredSkillsOrManifest(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Status:      "ready",
		Settings:    map[string]any{},
	}
	packet := CompileAgentContext(AgentContextInput{
		Project:    project,
		Message:    "dark mode is broken",
		IsFirstGen: false,
	})

	if len(packet.SkillIDs) != 0 {
		t.Fatalf("existing edit should not load required skill bodies, got %v", packet.SkillIDs)
	}

	combined := ""
	for _, msg := range packet.SystemMessages {
		combined += msg.Content + "\n"
	}
	for _, forbidden := range []string{
		"AVAILABLE SKILLS",
		"# SKILL:",
		"Tailwind v4 UI System",
		"Web Builder Foundation",
		"production-grade web apps",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("existing edit context contains prompt bloat %q:\n%s", forbidden, combined)
		}
	}
	if !strings.Contains(combined, "smallest correct change") {
		t.Fatalf("existing edit prompt should emphasize narrow fixes:\n%s", combined)
	}
}

func TestCompileAgentContext_WebEditCollapsesDuplicatedEditPrompts(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Status:      "ready",
		Settings:    map[string]any{},
	}
	packet := CompileAgentContext(AgentContextInput{
		Project:    project,
		Message:    "dark mode is broken",
		IsFirstGen: false,
	})

	var planningBlocks, leanGuidanceBlocks int
	for _, msg := range packet.SystemMessages {
		if strings.Contains(msg.Content, "## Planning Discipline") {
			planningBlocks++
		}
		if strings.Contains(msg.Content, "## Existing Edit Guidance") {
			leanGuidanceBlocks++
		}
	}
	if planningBlocks != 0 {
		t.Fatalf("existing web edits should not append a separate planning prompt, got %d", planningBlocks)
	}
	if leanGuidanceBlocks != 0 {
		t.Fatalf("existing web edits should not append a separate lean guidance prompt, got %d", leanGuidanceBlocks)
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

func TestRuntimeContextCompilerReturnsContextPacket(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp, Status: "ready"}
	compiler := RuntimeContextCompiler{}

	packet := compiler.Compile(AgentContextInput{
		Context:    context.Background(),
		Project:    project,
		Message:    "make it nicer",
		IsFirstGen: false,
	})

	if packet.Profile == nil {
		t.Fatal("expected context packet profile")
	}
	if len(packet.SystemMessages) == 0 {
		t.Fatal("expected at least one system message")
	}
}
