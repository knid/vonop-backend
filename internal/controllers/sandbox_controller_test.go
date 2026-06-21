package controllers

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestAddSandboxEnvFilesWritesBackendEnvForSplitWeb(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		BackendEnabled:  true,
		BackendRuntime:  models.BackendRuntimeNextJSAPI,
	}
	files := map[string]string{"backend/package.json": "{}"}
	env := map[string]string{"DATABASE_URL": "postgres://example", "NEXT_PUBLIC_SITE_URL": "https://example.test"}

	addSandboxEnvFiles(project, files, env)

	if _, ok := files[".env.local"]; ok {
		t.Fatalf("root .env.local should not be written for split web")
	}
	if got := files["backend/.env.local"]; got == "" {
		t.Fatalf("backend .env.local was not written")
	} else if !containsAll(got, []string{"DATABASE_URL=postgres://example", "NEXT_PUBLIC_SITE_URL=https://example.test"}) {
		t.Fatalf("backend .env.local = %q", got)
	}
}

func TestAddSandboxEnvFilesWritesRootEnvForSingleNext(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeNextJSLegacy,
	}
	files := map[string]string{"package.json": "{}"}

	addSandboxEnvFiles(project, files, map[string]string{"DATABASE_URL": "postgres://example"})

	if got := files[".env.local"]; got != "DATABASE_URL=postgres://example\n" {
		t.Fatalf(".env.local = %q", got)
	}
}

func TestAddSandboxEnvFilesClearsStaleEnvForSplitWeb(t *testing.T) {
	project := &models.AppProject{
		ID:              uuid.New(),
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		BackendEnabled:  true,
		BackendRuntime:  models.BackendRuntimeNextJSAPI,
	}
	files := map[string]string{"backend/.env.local": "DATABASE_URL=old\n"}

	addSandboxEnvFiles(project, files, nil)

	if got, ok := files["backend/.env.local"]; !ok || got != "" {
		t.Fatalf("backend .env.local = %q, exists=%v", got, ok)
	}
}

func TestSandboxExecOutputMarksInfrastructureError(t *testing.T) {
	out := sandboxExecOutput("", "", 0, "sandbox-1", errors.New("sandbox is not started"))

	if out["error"] != "sandbox is not started" {
		t.Fatalf("error = %#v", out["error"])
	}
	if out["exit_code"] != -1 {
		t.Fatalf("exit_code = %#v", out["exit_code"])
	}
}

func TestAdoptLatestSandboxIDCopiesPersistedIDWhenCurrentProjectIsStale(t *testing.T) {
	project := &models.AppProject{ID: uuid.New()}
	latest := &models.AppProject{ID: project.ID, SandboxID: "daytona-live"}

	if !adoptLatestSandboxID(project, latest) {
		t.Fatalf("expected sandbox id to be adopted")
	}
	if project.SandboxID != "daytona-live" {
		t.Fatalf("sandbox id = %q", project.SandboxID)
	}
}

func TestAdoptLatestSandboxIDDoesNotOverwriteExistingID(t *testing.T) {
	project := &models.AppProject{ID: uuid.New(), SandboxID: "daytona-current"}
	latest := &models.AppProject{ID: project.ID, SandboxID: "daytona-new"}

	if adoptLatestSandboxID(project, latest) {
		t.Fatalf("did not expect sandbox id to be adopted")
	}
	if project.SandboxID != "daytona-current" {
		t.Fatalf("sandbox id = %q", project.SandboxID)
	}
}

func containsAll(s string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}
