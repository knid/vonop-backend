package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestLoadEmbeddedSkillCatalog(t *testing.T) {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedSkillCatalog() error = %v", err)
	}
	if len(catalog) < 4 {
		t.Fatalf("expected built-in skills, got %d", len(catalog))
	}

	ids := skillIDs(catalog)
	for _, id := range []string{"accessibility", "nextjs-app-router", "tailwind-v4", "web-builder-foundation", "web-seo", "expo-router", "mobile-builder-foundation", "nativewind-ui"} {
		if !slices.Contains(ids, id) {
			t.Fatalf("expected skill id %q in catalog: %v", id, ids)
		}
	}
}

func TestResolveSkills_DefaultsAreProfileScoped(t *testing.T) {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedSkillCatalog() error = %v", err)
	}

	webProject := &models.AppProject{ProjectType: models.ProjectTypeApp}
	webSkills := resolveSkillsFromCatalog(NewSkillCatalog(catalog), ResolveAgentProfile(webProject), webProject, "make a CRM", false)
	webIDs := skillIDs(webSkills)
	if !slices.Contains(webIDs, "web-builder-foundation") {
		t.Fatalf("expected web default skill, got %v", webIDs)
	}
	if !slices.Contains(webIDs, "nextjs-app-router") || !slices.Contains(webIDs, "tailwind-v4") {
		t.Fatalf("expected web framework default skills, got %v", webIDs)
	}
	if slices.Contains(webIDs, "mobile-builder-foundation") {
		t.Fatalf("web builder must not load mobile default skill: %v", webIDs)
	}

	mobileProject := &models.AppProject{ProjectType: models.ProjectTypeMobileAppNative}
	mobileSkills := resolveSkillsFromCatalog(NewSkillCatalog(catalog), ResolveAgentProfile(mobileProject), mobileProject, "make a tracker", false)
	mobileIDs := skillIDs(mobileSkills)
	if !slices.Contains(mobileIDs, "mobile-builder-foundation") {
		t.Fatalf("expected mobile default skill, got %v", mobileIDs)
	}
	if !slices.Contains(mobileIDs, "expo-router") || !slices.Contains(mobileIDs, "nativewind-ui") {
		t.Fatalf("expected mobile framework default skills, got %v", mobileIDs)
	}
	if slices.Contains(mobileIDs, "web-builder-foundation") {
		t.Fatalf("mobile builder must not load web default skill: %v", mobileIDs)
	}
}

func TestResolveSkills_SlashCommandAndTriggers(t *testing.T) {
	catalog, err := LoadEmbeddedSkillCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedSkillCatalog() error = %v", err)
	}

	project := &models.AppProject{ProjectType: models.ProjectTypeApp}
	profile := ResolveAgentProfile(project)

	seoIDs := skillIDs(resolveSuggestedSkillsFromCatalog(NewSkillCatalog(catalog), profile, project, "/seo", false))
	if !slices.Contains(seoIDs, "web-seo") {
		t.Fatalf("/seo should activate web-seo, got %v", seoIDs)
	}

	a11yIDs := skillIDs(resolveSuggestedSkillsFromCatalog(NewSkillCatalog(catalog), profile, project, "improve accessibility and keyboard flow", false))
	if !slices.Contains(a11yIDs, "accessibility") {
		t.Fatalf("accessibility trigger should activate accessibility skill, got %v", a11yIDs)
	}
}

func TestRenderSkillManifestIncludesCompactMetadata(t *testing.T) {
	skills := []SkillFile{
		{ID: "foundation", Name: "Foundation", Description: "Required.", Agents: []string{AgentIDWebBuilder}},
		{ID: "seo", Name: "SEO", Description: "SEO guidance.", Agents: []string{AgentIDWebBuilder}, Triggers: []string{"seo"}, RiskLevel: "low", Body: "full body should not appear"},
	}
	msg := RenderSkillManifestMessage(skills, skills[:1], skills[1:])
	if msg.Role != "system" {
		t.Fatalf("manifest role = %q, want system", msg.Role)
	}
	if !strings.Contains(msg.Content, "id: seo") || !strings.Contains(msg.Content, "triggers: seo") {
		t.Fatalf("manifest missing seo metadata:\n%s", msg.Content)
	}
	if strings.Contains(msg.Content, "full body should not appear") {
		t.Fatalf("manifest should not include full skill body:\n%s", msg.Content)
	}
}

func TestSkillRenderIncludesMetadataAndBody(t *testing.T) {
	skill := SkillFile{
		ID:          "demo",
		Name:        "Demo Skill",
		Description: "Does a thing.",
		Origin:      "test",
		Body:        "## Instructions\n\n- Act carefully.",
	}
	rendered := skill.Render()
	for _, want := range []string{"# SKILL: Demo Skill", "Description: Does a thing.", "Origin: test", "- Act carefully."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered skill missing %q:\n%s", want, rendered)
		}
	}
}
