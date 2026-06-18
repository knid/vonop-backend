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
	if names["patch_files"] {
		t.Fatal("patch_files must not be exposed; use multi_edit for single-file and multi-file exact-string edits")
	}
	if !names["multi_edit"] {
		t.Fatal("expected multi_edit to be exposed")
	}
	if names["update_todos"] {
		t.Fatal("update_todos must not be exposed by default; failed todo calls waste agent rounds")
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
