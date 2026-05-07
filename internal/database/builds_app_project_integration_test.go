//go:build integration

// Integration tests for the v1-shadow-Project removal Phase 1 helpers
// (GetLastBuildByAppProject, GetBuildsByAppProject,
// GetLastSuccessfulBuildByAppProject, GetPendingAppBuildTasksByAppProject,
// UpsertMobileAppConfigOneSignal). These pin the new direct-FK scope so
// Task 2 can swap the controllers over to AppProject-keyed lookups without
// regression risk.
package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// seedAppProjectFixtures inserts a User, a personal Organization, an
// AppProject (the new direct-FK target) and a v1 Project (still required
// to satisfy the legacy NOT-NULL-dropped-but-still-present project_id
// columns on Build / AppBuildTask). Returns the AppProject + v1 Project so
// callers can attach builds/tasks. The v1 project URL is keyed off the
// AppProject id so each call produces a unique row.
func seedAppProjectFixtures(t *testing.T, db *Database) (*models.AppProject, *models.Project, *models.User) {
	t.Helper()
	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com",
		Name: "Owner", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	org := &models.Organization{
		ID: uuid.New(), Name: "Personal " + uuid.NewString()[:6],
		Slug: "org-" + uuid.NewString()[:8], OwnerID: user.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	appProject := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "App " + uuid.NewString()[:6],
		Slug: "app-" + uuid.NewString()[:8],
		Prompt: "test", ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(appProject).Exec(ctx); err != nil {
		t.Fatalf("insert app project: %v", err)
	}

	v1project := &models.Project{
		ID: uuid.New(), UserID: user.ID,
		URL:               "expo://" + appProject.Slug,
		PackageName:       "io.acme." + uuid.NewString()[:8],
		IconPath:          "x",
		OneSignalAppID:    uuid.NewString(),
		OneSignalAppToken: uuid.NewString(),
	}
	if _, err := db.DB.NewInsert().Model(v1project).Exec(ctx); err != nil {
		t.Fatalf("insert v1 project: %v", err)
	}

	return appProject, v1project, user
}

// TestGetLastBuildByAppProject_ReturnsHighestNumberedAndIgnoresOtherProjects
// pins the next-build-number contract. BuildExpoMobile in Task 2 will use
// this to compute build_number = last+1 without going through v1 Project.
func TestGetLastBuildByAppProject_ReturnsHighestNumberedAndIgnoresOtherProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
	)

	ctx := context.Background()
	appProj, v1proj, _ := seedAppProjectFixtures(t, db)
	otherAppProj, otherV1proj, _ := seedAppProjectFixtures(t, db)

	mkBuild := func(appPID *uuid.UUID, v1PID uuid.UUID, num int) *models.Build {
		b := &models.Build{
			ID: uuid.New(), ProjectID: v1PID, AppProjectID: appPID,
			Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
			NavBarStyle: "default", BuildNumber: num,
			VersionString: "1.0." + uuid.NewString()[:4],
		}
		if _, err := db.DB.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatalf("insert build %d: %v", num, err)
		}
		return b
	}

	mkBuild(&appProj.ID, v1proj.ID, 1)
	want := mkBuild(&appProj.ID, v1proj.ID, 7) // highest for our project
	mkBuild(&appProj.ID, v1proj.ID, 3)
	mkBuild(&otherAppProj.ID, otherV1proj.ID, 99) // higher, but other project

	got, err := db.GetLastBuildByAppProject(appProj.ID.String())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Errorf("got build %+v want %s (build_number=7)", got, want.ID)
	}
	if got.BuildNumber != 7 {
		t.Errorf("build_number: got %d want 7", got.BuildNumber)
	}
}

// TestGetBuildsByAppProject_ReturnsBuildsInDescOrder pins the
// read_build_logs ordering contract — agent expects newest-first.
func TestGetBuildsByAppProject_ReturnsBuildsInDescOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
	)

	ctx := context.Background()
	appProj, v1proj, _ := seedAppProjectFixtures(t, db)

	mkBuild := func(num int) {
		b := &models.Build{
			ID: uuid.New(), ProjectID: v1proj.ID, AppProjectID: &appProj.ID,
			Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
			NavBarStyle: "default", BuildNumber: num,
			VersionString: "1.0." + uuid.NewString()[:4],
		}
		if _, err := db.DB.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatalf("insert build %d: %v", num, err)
		}
	}

	mkBuild(1)
	mkBuild(5)
	mkBuild(3)

	got, err := db.GetBuildsByAppProject(appProj.ID.String())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d builds want 3", len(got))
	}
	if got[0].BuildNumber != 5 || got[1].BuildNumber != 3 || got[2].BuildNumber != 1 {
		t.Errorf("ordering wrong: got [%d,%d,%d] want [5,3,1]",
			got[0].BuildNumber, got[1].BuildNumber, got[2].BuildNumber)
	}
}

// TestGetLastSuccessfulBuildByAppProject_OnlyReturnsBuiltAndFinished is the
// SubmitExpoMobile contract: a build is submittable only when at least one
// AppBuildTask has status_text="Built" and finished=true. Failed-only
// builds, mid-build ("Built" but not finished), and soft-deleted tasks
// must all be excluded.
func TestGetLastSuccessfulBuildByAppProject_OnlyReturnsBuiltAndFinished(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	appProj, v1proj, _ := seedAppProjectFixtures(t, db)

	mkBuild := func(num int) *models.Build {
		b := &models.Build{
			ID: uuid.New(), ProjectID: v1proj.ID, AppProjectID: &appProj.ID,
			Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
			NavBarStyle: "default", BuildNumber: num,
			VersionString: "1.0." + uuid.NewString()[:4],
		}
		if _, err := db.DB.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatalf("insert build %d: %v", num, err)
		}
		return b
	}

	mkTask := func(b *models.Build, status string, finished bool, softDeleted bool) {
		task := &models.AppBuildTask{
			ID: uuid.New(), ProjectID: v1proj.ID, AppProjectID: &appProj.ID,
			BuildID: b.ID, Platform: "Android",
			StatusText: status, Finished: finished,
			CreatedAt: time.Now(),
		}
		if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if softDeleted {
			if _, err := db.DB.NewUpdate().
				TableExpr("app_build_tasks").
				Set("deleted_at = current_timestamp").
				Where("id = ?", task.ID).
				Exec(ctx); err != nil {
				t.Fatalf("soft delete: %v", err)
			}
		}
	}

	// Build 1: only failed task
	b1 := mkBuild(1)
	mkTask(b1, "Failed", true, false)

	// Build 2: only mid-build (Built but unfinished)
	b2 := mkBuild(2)
	mkTask(b2, "Built", false, false)

	// Build 3: built+finished, but soft-deleted task
	b3 := mkBuild(3)
	mkTask(b3, "Built", true, true)

	// Build 4: legitimately succeeded — this one should win
	b4 := mkBuild(4)
	mkTask(b4, "Built", true, false)

	// Build 5: an EVEN higher build number, but it's not yet successful.
	// The lookup should still return b4 because b5 has no Built+finished
	// task.
	b5 := mkBuild(5)
	mkTask(b5, "Failed", true, false)

	got, err := db.GetLastSuccessfulBuildByAppProject(appProj.ID.String())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.ID != b4.ID {
		t.Errorf("got build %+v want %s (build_number=4)", got, b4.ID)
	}
	if got.BuildNumber != 4 {
		t.Errorf("build_number: got %d want 4", got.BuildNumber)
	}
	_ = b5 // referenced above but not directly asserted; silence linters
}

// TestGetPendingAppBuildTasksByAppProject_ReturnsOnlyUnfinishedNativeTasks
// pins the concurrent-build mutex contract. BuildExpoMobile uses this to
// reject a new build trigger while another is still in flight.
func TestGetPendingAppBuildTasksByAppProject_ReturnsOnlyUnfinishedNativeTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	appProj, v1proj, _ := seedAppProjectFixtures(t, db)
	otherAppProj, otherV1proj, _ := seedAppProjectFixtures(t, db)

	mkBuild := func(appPID *uuid.UUID, v1PID uuid.UUID, num int) *models.Build {
		b := &models.Build{
			ID: uuid.New(), ProjectID: v1PID, AppProjectID: appPID,
			Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
			NavBarStyle: "default", BuildNumber: num,
			VersionString: "1.0." + uuid.NewString()[:4],
		}
		if _, err := db.DB.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatalf("insert build: %v", err)
		}
		return b
	}

	b1 := mkBuild(&appProj.ID, v1proj.ID, 1)
	otherB := mkBuild(&otherAppProj.ID, otherV1proj.ID, 1)

	mkTask := func(appPID *uuid.UUID, v1PID uuid.UUID, b *models.Build, platform string, finished bool) *models.AppBuildTask {
		task := &models.AppBuildTask{
			ID: uuid.New(), ProjectID: v1PID, AppProjectID: appPID,
			BuildID: b.ID, Platform: platform,
			StatusText: "Queued", Finished: finished,
			CreatedAt: time.Now(),
		}
		if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		return task
	}

	// Pending Android — should be returned
	pendingAndroid := mkTask(&appProj.ID, v1proj.ID, b1, "Android", false)
	// Pending iOS — should be returned
	pendingIOS := mkTask(&appProj.ID, v1proj.ID, b1, "IOS", false)
	// Finished — should be excluded
	mkTask(&appProj.ID, v1proj.ID, b1, "Android", true)
	// Other app project — should be excluded
	mkTask(&otherAppProj.ID, otherV1proj.ID, otherB, "Android", false)

	got, err := db.GetPendingAppBuildTasksByAppProject(appProj.ID.String())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks want 2 (only pending Android+iOS for our project)", len(got))
	}
	seen := map[uuid.UUID]bool{}
	for _, task := range got {
		seen[task.ID] = true
	}
	if !seen[pendingAndroid.ID] {
		t.Errorf("pendingAndroid missing")
	}
	if !seen[pendingIOS.ID] {
		t.Errorf("pendingIOS missing")
	}
}

// TestUpsertMobileAppConfigOneSignal_CreatesThenUpdates pins the idempotent
// upsert contract: first call inserts, second call on the same project_id
// updates the same row in place.
func TestUpsertMobileAppConfigOneSignal_CreatesThenUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.AppProject)(nil),
		(*models.Project)(nil),
		(*models.MobileAppConfig)(nil),
	)

	ctx := context.Background()
	appProj, _, _ := seedAppProjectFixtures(t, db)

	credRef1 := &models.CredRefJSON{
		OrgID: uuid.New(), Kind: "onesignal_rest_key",
		BlobS3Key: "blob/v1", DEKBlobS3Key: "dek/v1",
	}
	cfg1, err := db.UpsertMobileAppConfigOneSignal(ctx, appProj.ID, "os-app-id-v1", credRef1)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if cfg1 == nil || cfg1.OneSignalAppID != "os-app-id-v1" {
		t.Errorf("first upsert: got %+v", cfg1)
	}
	if cfg1.OneSignalRESTKeyCredRef == nil || cfg1.OneSignalRESTKeyCredRef.BlobS3Key != "blob/v1" {
		t.Errorf("first upsert credref: got %+v", cfg1.OneSignalRESTKeyCredRef)
	}

	// Second upsert — same project_id, different values. Must update in
	// place, not insert a duplicate row.
	credRef2 := &models.CredRefJSON{
		OrgID: credRef1.OrgID, Kind: "onesignal_rest_key",
		BlobS3Key: "blob/v2", DEKBlobS3Key: "dek/v2",
	}
	cfg2, err := db.UpsertMobileAppConfigOneSignal(ctx, appProj.ID, "os-app-id-v2", credRef2)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if cfg2 == nil || cfg2.OneSignalAppID != "os-app-id-v2" {
		t.Errorf("second upsert: got %+v", cfg2)
	}
	if cfg2.OneSignalRESTKeyCredRef == nil || cfg2.OneSignalRESTKeyCredRef.BlobS3Key != "blob/v2" {
		t.Errorf("second upsert credref: got %+v", cfg2.OneSignalRESTKeyCredRef)
	}

	// Confirm only one row exists for this project_id (idempotency).
	count, err := db.DB.NewSelect().
		Model((*models.MobileAppConfig)(nil)).
		Where("project_id = ?", appProj.ID).
		Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("row count: got %d want 1 (upsert must be idempotent)", count)
	}
}
