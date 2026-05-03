//go:build integration

package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/services/gitrepo"
)

// setupMobileBootstrapTest creates the shared fixture for the bootstrap
// tests: a fresh DB with the required tables, a controller wired with a
// gitrepo service rooted at t.TempDir(), an owner user, an org, and an
// owner membership. No StandardUserLimits row is inserted — GetUserLimits
// returns an error, the warn-and-continue branch fires, and the limit
// check is skipped (which is what we want for a no-quota test).
func setupMobileBootstrapTest(t *testing.T) (*Controller, *database.Database, *models.User, *models.Organization) {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	// Inject a gitrepo service rooted at a per-test temp dir so the
	// scaffolding writes don't pollute /data/repos and tests are isolated.
	c.GitRepoService = gitrepo.NewService(t.TempDir())

	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Name: "Owner", Email: "owner@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID:      uuid.New(),
		Name:    "Acme",
		Slug:    "acme-mb-" + uuid.NewString()[:8],
		OwnerID: user.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	return c, db, user, org
}

// TestCreateAppProject_MobileAppNative_SeedsMobileAppConfig verifies that a
// mobile_app_native project gets an eager MobileAppConfig row with the
// expected boolean defaults populated (PushEnabled, BridgeEnabled, etc.).
func TestCreateAppProject_MobileAppNative_SeedsMobileAppConfig(t *testing.T) {
	c, db, user, org := setupMobileBootstrapTest(t)
	ctx := context.Background()

	project, err := c.CreateAppProject(ctx, user, requests.GenerateAppRequest{
		Prompt:         "habit tracker mobile app with push notifications",
		ProjectType:    models.ProjectTypeMobileAppNative,
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("CreateAppProject: %v", err)
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		t.Fatalf("expected mobile_app_native, got %q", project.ProjectType)
	}

	cfg, err := db.GetMobileAppConfig(project.ID.String())
	if err != nil {
		t.Fatalf("expected eager MobileAppConfig row, got error: %v", err)
	}
	if cfg.ProjectID != project.ID {
		t.Errorf("MobileAppConfig.ProjectID mismatch: got %v want %v", cfg.ProjectID, project.ID)
	}
	if !cfg.PushEnabled {
		t.Error("default PushEnabled = true expected")
	}
	if !cfg.BridgeEnabled {
		t.Error("default BridgeEnabled = true expected")
	}
	if !cfg.AnalyticsEnabled {
		t.Error("default AnalyticsEnabled = true expected")
	}
	if !cfg.PullToRefreshEnabled {
		t.Error("default PullToRefreshEnabled = true expected")
	}
	if !cfg.ExternalBrowserLinks {
		t.Error("default ExternalBrowserLinks = true expected")
	}
	if !cfg.DarkModeEnabled {
		t.Error("default DarkModeEnabled = true expected")
	}
	if cfg.OfflineEnabled {
		t.Error("default OfflineEnabled = false expected")
	}
	if cfg.BiometricEnabled {
		t.Error("default BiometricEnabled = false expected")
	}
	if cfg.RatingEnabled {
		t.Error("default RatingEnabled = false expected")
	}
	if cfg.UpdateCheckEnabled {
		t.Error("default UpdateCheckEnabled = false expected")
	}
	if cfg.RatingSessions != 5 {
		t.Errorf("default RatingSessions = 5 expected, got %d", cfg.RatingSessions)
	}
}

// TestCreateAppProject_NonMobile_DoesNotSeedMobileAppConfig verifies that a
// non-mobile project (ecommerce here, to skip the LLM classifier path) does
// NOT get a MobileAppConfig row. Using ecommerce instead of "app" avoids a
// real network call to the LLM provider during the test.
func TestCreateAppProject_NonMobile_DoesNotSeedMobileAppConfig(t *testing.T) {
	c, db, user, org := setupMobileBootstrapTest(t)
	ctx := context.Background()

	project, err := c.CreateAppProject(ctx, user, requests.GenerateAppRequest{
		Prompt:         "online clothing store",
		ProjectType:    models.ProjectTypeEcommerce,
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("CreateAppProject: %v", err)
	}

	count, err := db.DB.NewSelect().Model((*models.MobileAppConfig)(nil)).
		Where("project_id = ?", project.ID).Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 MobileAppConfig rows for non-mobile project, got %d", count)
	}
}

// TestCreateAppProject_MobileAppNative_ScaffoldsRepoSkeleton verifies the
// canonical mobile/ + backend/ skeleton is committed to the project's git
// repo on creation, with a README.md mentioning the project name and slug.
func TestCreateAppProject_MobileAppNative_ScaffoldsRepoSkeleton(t *testing.T) {
	c, _, user, org := setupMobileBootstrapTest(t)
	ctx := context.Background()

	project, err := c.CreateAppProject(ctx, user, requests.GenerateAppRequest{
		Prompt:         "fitness tracker mobile app",
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
	if !repo.FileExists("mobile/.gitkeep") {
		t.Error("expected mobile/.gitkeep to exist in scaffold")
	}
	if !repo.FileExists("backend/.gitkeep") {
		t.Error("expected backend/.gitkeep to exist in scaffold")
	}
	if !repo.FileExists("README.md") {
		t.Error("expected README.md to exist in scaffold")
	}

	readme, err := repo.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := string(readme)
	if !strings.Contains(body, project.Name) {
		t.Errorf("README.md missing project Name %q; got: %s", project.Name, body)
	}
	if !strings.Contains(body, project.Slug) {
		t.Errorf("README.md missing project Slug %q; got: %s", project.Slug, body)
	}
	if !strings.Contains(body, "mobile/") {
		t.Errorf("README.md missing 'mobile/' subtree mention; got: %s", body)
	}
	if !strings.Contains(body, "backend/") {
		t.Errorf("README.md missing 'backend/' subtree mention; got: %s", body)
	}

	// Verify the scaffold commit landed in the repo log so a future
	// reset-to-empty doesn't accidentally pass this test.
	log, err := repo.Log(5)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(log) == 0 {
		t.Fatal("expected at least one commit in scaffolded repo")
	}
	foundScaffoldCommit := false
	for _, ci := range log {
		if strings.Contains(ci.Message, "scaffold") {
			foundScaffoldCommit = true
			break
		}
	}
	if !foundScaffoldCommit {
		t.Errorf("expected a 'scaffold' commit in log; got: %+v", log)
	}
}
