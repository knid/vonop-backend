//go:build integration

package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// TestCreateAppProject_MobileAppNative_ScaffoldsLandingPage verifies that the
// P3.1 landing-page baseline (backend/app/page.tsx + layout.tsx + globals.css)
// lands in the project's git repo on creation, alongside the existing
// mobile/.gitkeep + backend/.gitkeep + README.md skeleton from P1.1.B.
//
// The scaffold is deterministic Go — it interpolates project.Name + a
// truncated project.Prompt into the page hero. We assert on the project
// name appearing in both page.tsx and layout.tsx so a future "let's
// templatize this" refactor can't silently drop the substitution.
func TestCreateAppProject_MobileAppNative_ScaffoldsLandingPage(t *testing.T) {
	c, _, user, org := setupMobileBootstrapTest(t)
	ctx := context.Background()

	project, err := c.CreateAppProject(ctx, user, requests.GenerateAppRequest{
		Prompt:         "habit tracker mobile app with push notifications and streak rewards",
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

	// page.tsx
	if !repo.FileExists("backend/app/page.tsx") {
		t.Fatal("expected backend/app/page.tsx to exist")
	}
	pageBytes, err := repo.ReadFile("backend/app/page.tsx")
	if err != nil {
		t.Fatalf("read page.tsx: %v", err)
	}
	page := string(pageBytes)
	if !strings.Contains(page, project.Name) {
		t.Errorf("page.tsx missing project Name %q", project.Name)
	}
	if !strings.Contains(page, "Coming soon") {
		t.Errorf("page.tsx missing 'Coming soon' placeholder copy")
	}
	if !strings.Contains(page, "App Store") {
		t.Errorf("page.tsx missing App Store CTA")
	}
	if !strings.Contains(page, "Play Store") {
		t.Errorf("page.tsx missing Play Store CTA")
	}
	if !strings.Contains(page, "export default function") {
		t.Errorf("page.tsx missing default export — won't render under Next.js App Router")
	}

	// layout.tsx
	if !repo.FileExists("backend/app/layout.tsx") {
		t.Fatal("expected backend/app/layout.tsx to exist")
	}
	layoutBytes, err := repo.ReadFile("backend/app/layout.tsx")
	if err != nil {
		t.Fatalf("read layout.tsx: %v", err)
	}
	layout := string(layoutBytes)
	if !strings.Contains(layout, project.Name) {
		t.Errorf("layout.tsx missing project Name %q in metadata", project.Name)
	}
	if !strings.Contains(layout, "export const metadata") {
		t.Errorf("layout.tsx missing metadata export — Next.js head won't populate")
	}
	if !strings.Contains(layout, "openGraph") {
		t.Errorf("layout.tsx missing openGraph metadata")
	}
	if !strings.Contains(layout, "twitter") {
		t.Errorf("layout.tsx missing twitter metadata")
	}
	if !strings.Contains(layout, `import "./globals.css"`) {
		t.Errorf("layout.tsx missing globals.css import")
	}

	// globals.css
	if !repo.FileExists("backend/app/globals.css") {
		t.Fatal("expected backend/app/globals.css to exist")
	}

	// Skeleton was committed (so a later 'reset to empty' test couldn't
	// silently pass). The scaffold commit message changed from P1.1.B's
	// to call out the landing — guard the new message wording.
	log, err := repo.Log(5)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	foundLandingScaffoldCommit := false
	for _, ci := range log {
		if strings.Contains(ci.Message, "landing page") {
			foundLandingScaffoldCommit = true
			break
		}
	}
	if !foundLandingScaffoldCommit {
		t.Errorf("expected scaffold commit mentioning 'landing page'; got: %+v", log)
	}
}
