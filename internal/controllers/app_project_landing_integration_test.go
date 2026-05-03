//go:build integration

package controllers

import (
	"context"
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// TestCreateAppProject_MobileAppNative_DoesNotScaffoldBackend verifies that
// new mobile_app_native projects start with mobile/ ONLY — no backend/,
// no landing page, no Dockerfile. Most apps are pure client-side and never
// need a server; the agent creates backend/ explicitly via the write tool
// the first time the user asks for server-side functionality.
//
// (Replaces the prior P3.1 test that asserted automatic landing-page
// scaffolding — that behavior was reverted because it triggered the agent
// to proactively touch backend/ on every new project, including pure
// client-side apps like calculators.)
func TestCreateAppProject_MobileAppNative_DoesNotScaffoldBackend(t *testing.T) {
	c, _, user, org := setupMobileBootstrapTest(t)
	ctx := context.Background()

	project, err := c.CreateAppProject(ctx, user, requests.GenerateAppRequest{
		Prompt:         "simple calculator mobile app",
		ProjectType:    models.ProjectTypeMobileAppNative,
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("CreateAppProject: %v", err)
	}

	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	// mobile/ skeleton exists.
	if !repo.FileExists("mobile/.gitkeep") {
		t.Error("expected mobile/.gitkeep to exist as scaffold marker")
	}
	if !repo.FileExists("README.md") {
		t.Error("expected README.md to exist")
	}

	// backend/ does NOT exist by default. Any of these files would mean
	// we slipped back into the auto-scaffold pattern.
	for _, path := range []string{
		"backend/.gitkeep",
		"backend/app/page.tsx",
		"backend/app/layout.tsx",
		"backend/app/globals.css",
		"backend/Dockerfile",
		"backend/package.json",
	} {
		if repo.FileExists(path) {
			t.Errorf("%s should NOT exist on a freshly created mobile_app_native project — backend/ is opt-in", path)
		}
	}
}
