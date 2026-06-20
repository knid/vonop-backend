package agent

import (
	"context"
	"encoding/json"
	"strings"
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
	if !names["read_file_range"] {
		t.Fatal("expected read_file_range to be exposed")
	}
	if names["update_todos"] {
		t.Fatal("update_todos must not be exposed by default; failed todo calls waste agent rounds")
	}
}

func TestBuildRegistryFor_PrioritizesSearchAndRangesBeforeReadFiles(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, nil, "")
	names := registry.Names()
	index := func(target string) int {
		for i, name := range names {
			if name == target {
				return i
			}
		}
		t.Fatalf("missing tool %s in registry: %v", target, names)
		return -1
	}

	readFiles := index("read_files")
	for _, earlier := range []string{"grep", "read_file_range", "list_files"} {
		if got := index(earlier); got > readFiles {
			t.Fatalf("%s should be registered before read_files to bias away from broad reads; order=%v", earlier, names)
		}
	}
}

func TestRegistryExecuteRejectsInvalidInputBeforeToolExecution(t *testing.T) {
	registry := NewRegistry()
	registry.Register(GrepTool{})

	out, isError, err := registry.Execute(context.Background(), newPathScopeTestRC(models.ProjectTypeApp), "grep", json.RawMessage(`{"glob":"app/**/*.tsx"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected schema validation error, got: %s", out)
	}
	if !strings.Contains(out, "invalid grep input") || !strings.Contains(out, "pattern") {
		t.Fatalf("expected schema validation detail, got: %s", out)
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
	for _, name := range []string{"sandbox_start", "sandbox_sync", "sandbox_read_file", "sandbox_write_file", "sandbox_list_files", "read_file", "update_todos"} {
		if names[name] {
			t.Fatalf("%s must not be exposed", name)
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
