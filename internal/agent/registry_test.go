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
