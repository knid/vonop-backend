package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestSearchSkillsReturnsCompactMetadata(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp}
	rc := &RunContext{
		Profile:        ResolveAgentProfile(project),
		Project:        project,
		RequiredSkills: ResolveSkills(ResolveAgentProfile(project), project, "", false),
		SkillManifest:  ResolveCompatibleSkills(ResolveAgentProfile(project), project),
	}
	result, isErr, err := SearchSkillsTool{}.Execute(context.Background(), rc, json.RawMessage(`{"query":"seo"}`))
	if err != nil || isErr {
		t.Fatalf("search_skills error=%v isErr=%v result=%s", err, isErr, result)
	}
	if !strings.Contains(result, `"id": "web-seo"`) {
		t.Fatalf("expected web-seo search result, got:\n%s", result)
	}
	if strings.Contains(result, "## Instructions") {
		t.Fatalf("search_skills should not return full bodies:\n%s", result)
	}
}

func TestLoadSkillReturnsBodyAndMarksLoaded(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeApp}
	rc := &RunContext{
		Profile:        ResolveAgentProfile(project),
		Project:        project,
		RequiredSkills: ResolveSkills(ResolveAgentProfile(project), project, "", false),
		SkillManifest:  ResolveCompatibleSkills(ResolveAgentProfile(project), project),
	}
	rc.Skills = append([]SkillFile(nil), rc.RequiredSkills...)

	result, isErr, err := LoadSkillTool{}.Execute(context.Background(), rc, json.RawMessage(`{"id":"web-seo"}`))
	if err != nil || isErr {
		t.Fatalf("load_skill error=%v isErr=%v result=%s", err, isErr, result)
	}
	if !strings.Contains(result, "# SKILL:") || !strings.Contains(result, "web-seo") {
		t.Fatalf("load_skill should return full skill instructions, got:\n%s", result)
	}
	if !strings.Contains(strings.Join(rc.LoadedSkillIDs(), ","), "web-seo") {
		t.Fatalf("expected web-seo to be marked loaded, got %v", rc.LoadedSkillIDs())
	}
	if !strings.Contains(strings.Join(rc.ActiveSkillIDs(), ","), "web-seo") {
		t.Fatalf("expected web-seo to be active, got %v", rc.ActiveSkillIDs())
	}
}

func TestLoadSkillRejectsIncompatibleSkill(t *testing.T) {
	project := &models.AppProject{ProjectType: models.ProjectTypeMobileAppNative}
	rc := &RunContext{
		Profile:        ResolveAgentProfile(project),
		Project:        project,
		RequiredSkills: ResolveSkills(ResolveAgentProfile(project), project, "", false),
		SkillManifest:  ResolveCompatibleSkills(ResolveAgentProfile(project), project),
	}
	result, isErr, err := LoadSkillTool{}.Execute(context.Background(), rc, json.RawMessage(`{"id":"web-seo"}`))
	if err != nil {
		t.Fatalf("load_skill unexpected infra error: %v", err)
	}
	if !isErr || !strings.Contains(result, "not compatible") {
		t.Fatalf("expected incompatible skill error, isErr=%v result=%s", isErr, result)
	}
}
