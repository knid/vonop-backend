//go:build integration

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestLaunchpad_StoreTileUsesRelease verifies that the Launchpad store tile
// is populated from the latest Release row, not the legacy Deployment table
// (which was retired 2026-05-07 alongside the legacy submission pipeline).
//
// This pins the contract that ensures the Ship tab continues to show
// per-platform tile state after the legacy Deployment subsystem was removed.
func TestLaunchpad_StoreTileUsesRelease(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.Release)(nil),
		(*models.AppleConnection)(nil),
		(*models.GooglePlayConnection)(nil),
		(*models.MobileAppConfig)(nil),
		(*models.AppStoreListing)(nil),
		(*models.Build)(nil),
		(*models.AppDeployment)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{
		ID:    uuid.New(),
		Email: "release-tile@example.com",
	}
	if _, err := c.DB.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	org := &models.Organization{
		ID:         uuid.New(),
		Name:       "personal",
		OwnerID:    user.ID,
		IsPersonal: true,
	}
	if _, err := c.DB.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	project := &models.AppProject{
		ID:             uuid.New(),
		UserID:         user.ID,
		OrganizationID: org.ID,
		Name:           "Release Tile App",
		Slug:           "release-tile-app",
		ProjectType:    models.ProjectTypeMobileAppNative,
	}
	if _, err := c.DB.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	// Seed a "live" Release on iOS.
	iosRelease := &models.Release{
		ID:             uuid.New(),
		AppProjectID:   project.ID,
		OrganizationID: org.ID,
		Platform:       models.ReleasePlatformIOS,
		Track:          models.ReleaseTrackProduction,
		VersionCode:    1,
		Status:         models.ReleaseStatusLive,
		CreatedAt:      time.Now().Add(-24 * time.Hour),
		UpdatedAt:      time.Now().Add(-12 * time.Hour),
	}
	if _, err := c.DB.DB.NewInsert().Model(iosRelease).Exec(ctx); err != nil {
		t.Fatalf("seed release: %v", err)
	}

	// Seed an Apple connection so the tile has connection_kind.
	appleConn := &models.AppleConnection{
		ID:             uuid.New(),
		UserID:         user.ID,
		OrganizationID: org.ID,
	}
	if _, err := c.DB.DB.NewInsert().Model(appleConn).Exec(ctx); err != nil {
		t.Fatalf("seed apple conn: %v", err)
	}

	resp, err := c.GetLaunchpad(ctx, user, project.ID)
	if err != nil {
		t.Fatalf("GetLaunchpad: %v", err)
	}
	if resp == nil {
		t.Fatal("GetLaunchpad returned nil response")
	}

	if resp.Stores.AppStore.Status != "live" {
		t.Errorf("AppStore.Status = %q, want live", resp.Stores.AppStore.Status)
	}
	if resp.Stores.AppStore.LatestDeploymentID != iosRelease.ID.String() {
		t.Errorf("AppStore.LatestDeploymentID = %q, want %q",
			resp.Stores.AppStore.LatestDeploymentID, iosRelease.ID.String())
	}
	// Android has no release — should land in the no-release branch.
	if resp.Stores.PlayStore.Status == "live" {
		t.Errorf("PlayStore should not be live (no Android release seeded), got %q",
			resp.Stores.PlayStore.Status)
	}
}
