//go:build integration

package controllers

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestBackendDeployURL_UsesRuntimeAppDefaultDomain verifies that when a
// RuntimeApp row exists with the default {slug}.wemob.app CustomDomain,
// BackendDeployURL returns it (which happens to equal the canonical
// fallback — but goes through the runtime-app branch, not the fallback).
func TestBackendDeployURL_UsesRuntimeAppDefaultDomain(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.RuntimeApp)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{ID: uuid.New(), Email: "u@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "A", Slug: "a-bdu-" + uuid.NewString()[:8], OwnerID: user.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "acme-xy-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if _, err := db.DB.NewInsert().Model(&models.RuntimeApp{
		AppProjectID:  project.ID,
		GiteaRepoName: project.Slug,
		FlyAppName:    "wemob-" + project.Slug,
		Framework:     "nextjs",
		CustomDomain:  project.Slug + ".wemob.app",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert runtime app: %v", err)
	}

	got := c.BackendDeployURL(project)
	want := "https://" + project.Slug + ".wemob.app"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestBackendDeployURL_RuntimeAppWithCustomDomain verifies that a user-
// configured custom domain on the RuntimeApp is what BuildExpoMobile bakes
// into the binary, not the canonical wemob.app fallback.
func TestBackendDeployURL_RuntimeAppWithCustomDomain(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.RuntimeApp)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{ID: uuid.New(), Email: "u@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "A", Slug: "a-cd-" + uuid.NewString()[:8], OwnerID: user.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "acme-cd-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if _, err := db.DB.NewInsert().Model(&models.RuntimeApp{
		AppProjectID:  project.ID,
		GiteaRepoName: project.Slug,
		FlyAppName:    "wemob-" + project.Slug,
		Framework:     "nextjs",
		CustomDomain:  "api.acme.com",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert runtime app: %v", err)
	}

	got := c.BackendDeployURL(project)
	want := "https://api.acme.com"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
