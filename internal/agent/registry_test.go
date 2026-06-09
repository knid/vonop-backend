package agent

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestBuildRegistryFor_ExposesWriteFilesButNotWriteFile(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, nil, "")

	names := map[string]bool{}
	for _, name := range registry.Names() {
		names[name] = true
	}
	if names["write_file"] {
		t.Fatal("write_file must not be exposed; use write_files for all full-file writes")
	}
	if !names["write_files"] {
		t.Fatal("expected write_files to be exposed")
	}
}

func TestBuildRegistryFor_ExposesSandboxTools(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, &RunDependencies{}, "")

	names := map[string]bool{}
	for _, name := range registry.Names() {
		names[name] = true
	}
	for _, name := range sandboxToolNames() {
		if !names[name] {
			t.Fatalf("expected %s to be exposed", name)
		}
	}
}

func TestBuildRegistryFor_BackendOnlyExposesEnableFrontend(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeNone,
	}, &RunDependencies{}, "")

	names := map[string]bool{}
	for _, name := range registry.Names() {
		names[name] = true
	}
	if !names["enable_frontend"] {
		t.Fatal("expected backend-only app projects to expose enable_frontend")
	}
}

func TestFilterRegistryForDeepAgents_RemovesNativeOnlyTools(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, &RunDependencies{}, "")
	filtered := FilterRegistryForDeepAgents(registry)

	if filtered == nil {
		t.Fatal("expected non-nil filtered registry")
	}
	for _, excluded := range []string{
		"read_files", "list_files", "grep",
		"write_file", "write_files", "edit_file", "multi_edit", "patch_files", "delete_file",
		"list_skills", "search_skills", "load_skill",
		"update_todos", "create_design",
	} {
		for _, name := range filtered.Names() {
			if name == excluded {
				t.Fatalf("filtered registry must not expose %q (DeepAgents native owns it)", excluded)
			}
		}
	}
}

func TestFilterRegistryForDeepAgents_KeepsSandboxAndDataTools(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, &RunDependencies{}, "")
	filtered := FilterRegistryForDeepAgents(registry)

	names := map[string]bool{}
	for _, name := range filtered.Names() {
		names[name] = true
	}
	for _, expected := range []string{"sandbox_status", "sandbox_read_logs", "execute_sql", "provision_database", "read_attachment", "generate_image"} {
		if !names[expected] {
			t.Fatalf("expected filtered registry to keep %q", expected)
		}
	}
}
