package agent

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestBuildDeepAgentsToolSpecs_ExcludesFileAndSkillTools(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, &RunDependencies{}, "")
	filtered := FilterRegistryForDeepAgents(registry)
	specs := buildDeepAgentsToolSpecs(filtered)

	if len(specs) == 0 {
		t.Fatal("expected non-empty specs after filtering")
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, excluded := range []string{
		"read_files", "list_files", "grep",
		"write_file", "write_files", "edit_file", "multi_edit", "patch_files", "delete_file",
		"list_skills", "search_skills", "load_skill",
		"update_todos", "create_design",
	} {
		if names[excluded] {
			t.Fatalf("specs must not include %q (DeepAgents native owns it)", excluded)
		}
	}
}

func TestBuildDeepAgentsToolSpecs_AssignsRiskLevel(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, &RunDependencies{}, "")
	filtered := FilterRegistryForDeepAgents(registry)
	specs := buildDeepAgentsToolSpecs(filtered)

	if len(specs) == 0 {
		t.Fatal("expected non-empty specs")
	}
	for _, s := range specs {
		if s.RiskLevel == "" {
			t.Fatalf("spec %q has empty risk_level", s.Name)
		}
		if s.Description == "" {
			t.Fatalf("spec %q has empty description", s.Name)
		}
		if s.Name == "" {
			t.Fatalf("spec has empty name")
		}
		if s.Schema == nil {
			t.Fatalf("spec %q has nil schema", s.Name)
		}
	}
}
