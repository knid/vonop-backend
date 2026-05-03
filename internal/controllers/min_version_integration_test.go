//go:build integration

package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// minVersionFixture is the common test scaffolding for min_version tests:
// owner user + org + mobile_app_native project. Returns the controller, the
// owner, and the project so each test can extend the fixture (membership
// rows, MobileAppConfig rows, etc.) for its specific scenario.
func minVersionFixture(t *testing.T) (*Controller, *database.Database, *models.User, *models.Organization, *models.AppProject) {
	t.Helper()

	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	owner := &models.User{ID: uuid.New(), Email: "owner@example.com", Name: "Owner", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	org := &models.Organization{
		ID:      uuid.New(),
		Name:    "Acme",
		Slug:    "acme-mv-" + uuid.NewString()[:8],
		OwnerID: owner.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID,
		UserID:         owner.ID,
		Role:           "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	project := &models.AppProject{
		ID:             uuid.New(),
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Name:           "Test App",
		Slug:           "test-mv-" + uuid.NewString()[:8],
		Prompt:         "n/a",
		ProjectType:    models.ProjectTypeMobileAppNative,
		Status:         "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return c, db, owner, org, project
}

// TestGetMinVersion_HappyPath_iOS sets a per-platform minimum on the
// project's config and verifies MinVersionForPlatform returns it cleanly,
// honoring the customer's custom message.
func TestGetMinVersion_HappyPath_iOS(t *testing.T) {
	c, db, _, _, project := minVersionFixture(t)
	ctx := context.Background()

	if _, err := db.DB.NewInsert().Model(&models.MobileAppConfig{
		ProjectID:             project.ID,
		MinBuildNumberIOS:     42,
		MinBuildNumberAndroid: 17,
		MinVersionMessage:     "Critical fix.",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert config: %v", err)
	}

	got, err := c.GetProjectForPublicMobile(project.Slug)
	if err != nil {
		t.Fatalf("GetProjectForPublicMobile: %v", err)
	}
	if got.ID != project.ID {
		t.Errorf("project ID = %v, want %v", got.ID, project.ID)
	}

	minBuild, _, message, err := c.MinVersionForPlatform(got, "ios")
	if err != nil {
		t.Fatalf("MinVersionForPlatform: %v", err)
	}
	if minBuild != 42 {
		t.Errorf("min_build_number = %d, want 42", minBuild)
	}
	if message != "Critical fix." {
		t.Errorf("message = %q, want %q", message, "Critical fix.")
	}
}

// TestGetMinVersion_HappyPath_Android verifies the Android branch picks the
// Android column, not the iOS one.
func TestGetMinVersion_HappyPath_Android(t *testing.T) {
	c, db, _, _, project := minVersionFixture(t)
	ctx := context.Background()

	if _, err := db.DB.NewInsert().Model(&models.MobileAppConfig{
		ProjectID:             project.ID,
		MinBuildNumberIOS:     42,
		MinBuildNumberAndroid: 17,
	}).Exec(ctx); err != nil {
		t.Fatalf("insert config: %v", err)
	}

	minBuild, _, message, err := c.MinVersionForPlatform(project, "android")
	if err != nil {
		t.Fatalf("MinVersionForPlatform: %v", err)
	}
	if minBuild != 17 {
		t.Errorf("android min_build_number = %d, want 17", minBuild)
	}
	// No custom message → default English string surfaces.
	if message != "A new version is required. Please update to continue." {
		t.Errorf("default message wrong: %q", message)
	}
}

// TestGetProjectForPublicMobile_UnknownReturnsNotFound verifies the slug-
// then-ID resolver returns the sentinel error when neither lookup hits.
func TestGetProjectForPublicMobile_UnknownReturnsNotFound(t *testing.T) {
	c, _, _, _, _ := minVersionFixture(t)

	_, err := c.GetProjectForPublicMobile("does-not-exist-anywhere")
	if !errors.Is(err, ErrMinVersionProjectNotFound) {
		t.Errorf("err = %v, want ErrMinVersionProjectNotFound", err)
	}
}

// TestGetProjectForPublicMobile_FallsBackToID verifies the resolver finds a
// project by UUID when the slug lookup misses (the typical case is slug,
// but the mobile app might pass an ID in odd cases).
func TestGetProjectForPublicMobile_FallsBackToID(t *testing.T) {
	c, _, _, _, project := minVersionFixture(t)

	got, err := c.GetProjectForPublicMobile(project.ID.String())
	if err != nil {
		t.Fatalf("GetProjectForPublicMobile: %v", err)
	}
	if got.ID != project.ID {
		t.Errorf("project ID = %v, want %v", got.ID, project.ID)
	}
}

// TestMinVersionForPlatform_UnknownPlatform verifies the controller rejects
// invalid platform strings with a sentinel error.
func TestMinVersionForPlatform_UnknownPlatform(t *testing.T) {
	c, _, _, _, project := minVersionFixture(t)

	_, _, _, err := c.MinVersionForPlatform(project, "windows")
	if !errors.Is(err, ErrMinVersionUnknownPlatform) {
		t.Errorf("err = %v, want ErrMinVersionUnknownPlatform", err)
	}
}

// TestMinVersionForPlatform_MissingConfigReturnsZero verifies that a project
// with no MobileAppConfig row at all returns 0 (gate disabled) plus the
// default message — i.e. the read path is forgiving and never errors out
// just because the customer never touched the config.
func TestMinVersionForPlatform_MissingConfigReturnsZero(t *testing.T) {
	c, _, _, _, project := minVersionFixture(t)

	minBuild, storeURL, message, err := c.MinVersionForPlatform(project, "ios")
	if err != nil {
		t.Fatalf("MinVersionForPlatform: %v", err)
	}
	if minBuild != 0 {
		t.Errorf("min_build_number = %d, want 0", minBuild)
	}
	if storeURL != "" {
		t.Errorf("store_url = %q, want empty", storeURL)
	}
	if message == "" {
		t.Errorf("message should fall back to default, got empty string")
	}
}

// TestUpdateMinVersion_OwnerSucceeds verifies the happy path: an org owner
// can bump the floor and the change persists on MobileAppConfig.
func TestUpdateMinVersion_OwnerSucceeds(t *testing.T) {
	c, db, owner, _, project := minVersionFixture(t)
	ctx := context.Background()

	updated, err := c.UpdateMinVersion(owner, project.ID.String(), "ios", 99, "Please update.")
	if err != nil {
		t.Fatalf("UpdateMinVersion: %v", err)
	}
	if updated.MinBuildNumberIOS != 99 {
		t.Errorf("MinBuildNumberIOS = %d, want 99", updated.MinBuildNumberIOS)
	}
	if updated.MinVersionMessage != "Please update." {
		t.Errorf("MinVersionMessage = %q, want %q", updated.MinVersionMessage, "Please update.")
	}

	// Round-trip via the DB to guarantee the upsert wrote it.
	var got models.MobileAppConfig
	if err := db.DB.NewSelect().Model(&got).Where("project_id = ?", project.ID.String()).Scan(ctx); err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if got.MinBuildNumberIOS != 99 {
		t.Errorf("persisted MinBuildNumberIOS = %d, want 99", got.MinBuildNumberIOS)
	}
}

// TestUpdateMinVersion_AdminSucceeds verifies the role check accepts admin
// not just owner. Mirrors the MoveAppProjectToOrg pattern.
func TestUpdateMinVersion_AdminSucceeds(t *testing.T) {
	c, db, _, org, project := minVersionFixture(t)
	ctx := context.Background()

	admin := &models.User{ID: uuid.New(), Email: "admin@example.com", Name: "Admin", Password: "x"}
	if _, err := db.DB.NewInsert().Model(admin).Exec(ctx); err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID,
		UserID:         admin.ID,
		Role:           "admin",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert admin member: %v", err)
	}

	updated, err := c.UpdateMinVersion(admin, project.ID.String(), "android", 7, "")
	if err != nil {
		t.Fatalf("UpdateMinVersion (admin): %v", err)
	}
	if updated.MinBuildNumberAndroid != 7 {
		t.Errorf("MinBuildNumberAndroid = %d, want 7", updated.MinBuildNumberAndroid)
	}
}

// TestUpdateMinVersion_MemberRejected verifies the role check rejects a
// regular member. The view layer maps this to HTTP 403.
func TestUpdateMinVersion_MemberRejected(t *testing.T) {
	c, db, _, org, project := minVersionFixture(t)
	ctx := context.Background()

	member := &models.User{ID: uuid.New(), Email: "member@example.com", Name: "Member", Password: "x"}
	if _, err := db.DB.NewInsert().Model(member).Exec(ctx); err != nil {
		t.Fatalf("insert member user: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID,
		UserID:         member.ID,
		Role:           "member",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	_, err := c.UpdateMinVersion(member, project.ID.String(), "ios", 99, "")
	if !errors.Is(err, ErrMinVersionPermissionDenied) {
		t.Errorf("err = %v, want ErrMinVersionPermissionDenied", err)
	}
}

// TestUpdateMinVersion_NonMobileNativeRejected verifies the controller
// short-circuits non-mobile_app_native projects so we don't accidentally
// persist a min_version on a WebView/web project.
func TestUpdateMinVersion_NonMobileNativeRejected(t *testing.T) {
	c, db, owner, org, _ := minVersionFixture(t)
	ctx := context.Background()

	webProject := &models.AppProject{
		ID:             uuid.New(),
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Name:           "Web App",
		Slug:           "web-mv-" + uuid.NewString()[:8],
		Prompt:         "n/a",
		ProjectType:    "web", // anything that's not mobile_app_native
		Status:         "draft",
	}
	if _, err := db.DB.NewInsert().Model(webProject).Exec(ctx); err != nil {
		t.Fatalf("insert web project: %v", err)
	}

	_, err := c.UpdateMinVersion(owner, webProject.ID.String(), "ios", 99, "")
	if !errors.Is(err, ErrMinVersionNotMobileNative) {
		t.Errorf("err = %v, want ErrMinVersionNotMobileNative", err)
	}
}

// TestUpdateMinVersion_PreservesOtherPlatform verifies that updating just
// the iOS floor does not zero-out the Android floor (and vice-versa). This
// is the per-platform-specific bug the explicit "load existing config
// first" pattern in UpdateMinVersion guards against.
func TestUpdateMinVersion_PreservesOtherPlatform(t *testing.T) {
	c, db, owner, _, project := minVersionFixture(t)
	ctx := context.Background()

	// Seed both platforms.
	if _, err := db.DB.NewInsert().Model(&models.MobileAppConfig{
		ProjectID:             project.ID,
		MinBuildNumberIOS:     10,
		MinBuildNumberAndroid: 20,
	}).Exec(ctx); err != nil {
		t.Fatalf("insert config: %v", err)
	}

	// Update iOS only.
	if _, err := c.UpdateMinVersion(owner, project.ID.String(), "ios", 11, ""); err != nil {
		t.Fatalf("UpdateMinVersion ios: %v", err)
	}

	// Re-read; Android should still be 20.
	var got models.MobileAppConfig
	if err := db.DB.NewSelect().Model(&got).Where("project_id = ?", project.ID.String()).Scan(ctx); err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if got.MinBuildNumberIOS != 11 {
		t.Errorf("iOS = %d, want 11", got.MinBuildNumberIOS)
	}
	if got.MinBuildNumberAndroid != 20 {
		t.Errorf("Android = %d, want 20 (preserved across iOS update)", got.MinBuildNumberAndroid)
	}
}
