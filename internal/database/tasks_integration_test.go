//go:build integration

// Integration tests for the P0 #2 fix (mobile audit, 2026-05-04):
// status vocabulary unification across app_build_tasks. After P0 #1,
// successful Expo native builds write status_text="Built" but
// GetLatestSuccessfulBuildTaskByProjectAndPlatform was filtering on
// "Succeeded", so SubmitExpoMobile returned ErrSubmitNoBuildToSubmit
// for every native project. These tests pin the new IN-clause filter
// and the canonical "Failed" value the cron now writes.
package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// seedTaskFixtures inserts a User, a v1 Project, and a Build, returning
// the project + build so tests can attach AppBuildTask rows. Kept narrow
// — tests insert their own AppBuildTask rows so they can vary status,
// finished, and platform per-row.
func seedTaskFixtures(t *testing.T, db *Database) (*models.Project, *models.Build) {
	t.Helper()
	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Email: "owner-" + uuid.NewString()[:8] + "@e.com",
		Name: "Owner", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	project := &models.Project{
		ID: uuid.New(), UserID: user.ID,
		URL:               "expo://acme-" + uuid.NewString()[:8],
		PackageName:       "io.acme.test." + uuid.NewString()[:8],
		IconPath:          "x",
		OneSignalAppID:    uuid.NewString(),
		OneSignalAppToken: uuid.NewString(),
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	build := &models.Build{
		ID: uuid.New(), ProjectID: project.ID,
		Title: "T", Description: "D", Icon: "i", SplashScreenColor: "#fff",
		NavBarStyle: "default", BuildNumber: 1, VersionString: "1.0.0",
	}
	if _, err := db.DB.NewInsert().Model(build).Exec(ctx); err != nil {
		t.Fatalf("insert build: %v", err)
	}

	return project, build
}

// TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_ReturnsBuiltFinishedRow
// is the core P0 #2 regression test: a task written with the new canonical
// "Built" + finished=true MUST be returned by the lookup, otherwise
// SubmitExpoMobile cannot find any build to submit.
func TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_ReturnsBuiltFinishedRow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	project, build := seedTaskFixtures(t, db)

	task := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Built", Finished: true,
		CreatedAt: time.Now(),
	}
	if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	got, err := db.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, project.ID, "Android")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil {
		t.Fatalf("lookup returned nil; expected the Built+finished row")
	}
	if got.ID != task.ID {
		t.Errorf("got task %s, want %s", got.ID, task.ID)
	}
	if got.StatusText != "Built" {
		t.Errorf("status_text: got %q want Built", got.StatusText)
	}
	if !got.Finished {
		t.Errorf("finished: got false want true")
	}
}

// TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_RejectsBuiltButUnfinished
// pins the finished-gate semantics: a row in mid-build that has had its
// status_text optimistically set to "Built" but has NOT yet been marked
// finished=true must NOT be returned. Without this gate the lookup would
// pick up rows that are still streaming and SubmitExpoMobile would try to
// submit a build that doesn't have an artifact yet.
func TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_RejectsBuiltButUnfinished(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	project, build := seedTaskFixtures(t, db)

	// Mid-build row: status_text already says "Built" but the row has not
	// yet been flagged finished. Should not be selected.
	task := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Built", Finished: false,
		CreatedAt: time.Now(),
	}
	if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	got, err := db.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, project.ID, "Android")
	if err == nil {
		t.Fatalf("expected sql.ErrNoRows-style error, got task %+v", got)
	}
}

// TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_AcceptsLegacySucceeded
// is the historical-row safety net: rows written before P0 #1 (or by the
// legacy v1 reports path with status="Succeeded") must still be returned.
// The IN clause keeps this working without a backfill migration.
func TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_AcceptsLegacySucceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	project, build := seedTaskFixtures(t, db)

	task := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "IOS", StatusText: "Succeeded", Finished: true,
		CreatedAt: time.Now(),
	}
	if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	got, err := db.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, project.ID, "IOS")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.ID != task.ID {
		t.Errorf("legacy Succeeded row not returned; got %+v want %s", got, task.ID)
	}
}

// TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_PrefersMostRecent
// pins the ORDER BY contract — when two finished+successful rows exist for
// the same (project, platform), the more-recent one wins regardless of
// which canonical value it carries.
func TestGetLatestSuccessfulBuildTaskByProjectAndPlatform_PrefersMostRecent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	project, build := seedTaskFixtures(t, db)

	older := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Succeeded", Finished: true,
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if _, err := db.DB.NewInsert().Model(older).Exec(ctx); err != nil {
		t.Fatalf("insert older: %v", err)
	}
	newer := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Built", Finished: true,
		CreatedAt: time.Now(),
	}
	if _, err := db.DB.NewInsert().Model(newer).Exec(ctx); err != nil {
		t.Fatalf("insert newer: %v", err)
	}

	got, err := db.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, project.ID, "Android")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.ID != newer.ID {
		t.Errorf("got %+v want most-recent %s", got, newer.ID)
	}
}

// TestMarkOrphanedBuildTasksFailed_WritesCanonicalFailed pins the
// vocabulary fix: stale unfinished rows must be terminated with the
// canonical "Failed" value (was lowercase "error" before P0 #2).
func TestMarkOrphanedBuildTasksFailed_WritesCanonicalFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	db := NewTestDB(t,
		(*models.User)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
	)

	ctx := context.Background()
	project, build := seedTaskFixtures(t, db)

	// Insert a stuck row with created_at 2h in the past.
	stuck := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Queued", Finished: false,
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if _, err := db.DB.NewInsert().Model(stuck).Exec(ctx); err != nil {
		t.Fatalf("insert stuck: %v", err)
	}

	// And a fresh row that should NOT be reaped.
	fresh := &models.AppBuildTask{
		ID: uuid.New(), ProjectID: project.ID, BuildID: build.ID,
		Platform: "Android", StatusText: "Queued", Finished: false,
		CreatedAt: time.Now(),
	}
	if _, err := db.DB.NewInsert().Model(fresh).Exec(ctx); err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	n, err := db.MarkOrphanedBuildTasksFailed(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected: got %d want 1 (only the stuck row should be reaped)", n)
	}

	var got models.AppBuildTask
	if err := db.DB.NewSelect().Model(&got).Where("id = ?", stuck.ID).Scan(ctx); err != nil {
		t.Fatalf("reload stuck: %v", err)
	}
	if got.StatusText != "Failed" {
		t.Errorf("status_text: got %q want Failed (canonical post-P0 #2)", got.StatusText)
	}
	if !got.Finished {
		t.Errorf("finished: got false want true")
	}
	if got.ErrorMessage == "" {
		t.Errorf("error_message: got empty want timeout sentinel")
	}

	// Cross-check: the fresh row was not touched.
	var freshGot models.AppBuildTask
	if err := db.DB.NewSelect().Model(&freshGot).Where("id = ?", fresh.ID).Scan(ctx); err != nil {
		t.Fatalf("reload fresh: %v", err)
	}
	if freshGot.Finished {
		t.Errorf("fresh row was incorrectly reaped")
	}
	if freshGot.StatusText != "Queued" {
		t.Errorf("fresh row status_text overwritten: got %q", freshGot.StatusText)
	}

	// Bonus: the just-reaped stuck row is now visible to
	// GetLatestSuccessfulBuildTaskByProjectAndPlatform's anti-target — it
	// must NOT be returned (status=Failed is not in the success set).
	if hit, _ := db.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, project.ID, "Android"); hit != nil {
		t.Errorf("reaped row leaked into success lookup: %+v", hit)
	}
}
