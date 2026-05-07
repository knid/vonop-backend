package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateAppBuildTask(build models.Build, appProjectID uuid.UUID, platform, status string, autoDeploy bool) (*models.AppBuildTask, error) {
	appBuildTask := models.AppBuildTask{
		BuildID:      build.ID,
		AppProjectID: appProjectID,
		Platform:     platform,
		StatusText:   status,
		AutoDeploy:   autoDeploy,
	}
	_, err := db.DB.NewInsert().Model(&appBuildTask).Exec(context.Background())

	return &appBuildTask, err
}

// GetAppBuildTasksByUser returns every build task owned by `user` (via the
// AppProject ownership chain). Mobile_app_native builds carry app_project_id;
// the legacy v1 join was dropped because mobile builds now have project_id =
// NULL.
func (db *Database) GetAppBuildTasksByUser(user *models.User) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).
		Join("JOIN app_projects ap ON ap.id = app_build_task.app_project_id").
		Where("ap.user_id = ?", user.ID.String()).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return appBuildTasks, nil
}

// CountAppBuildTasksByOrgInPeriod counts native (Android/iOS) build tasks
// triggered for any AppProject in the given org within [start, end). Used by
// the MontlyBuildCount enforcement gate (A3 — previously theatrical).
//
// Joins through app_projects.organization_id so every member's builds count
// against the same monthly budget.
func (db *Database) CountAppBuildTasksByOrgInPeriod(ctx context.Context, orgID string, start, end time.Time) (int, error) {
	var count int
	err := db.DB.NewSelect().
		TableExpr("app_build_tasks AS abt").
		Join("JOIN app_projects ap ON ap.id = abt.app_project_id").
		ColumnExpr("COUNT(*)").
		Where("ap.organization_id = ?", orgID).
		Where("abt.platform IN ('Android','IOS')").
		Where("abt.created_at >= ?", start).
		Where("abt.created_at < ?", end).
		Scan(ctx, &count)
	return count, err
}

// GetAppBuildTasksByUserAndAppProject scopes the task list to a single
// AppProject owned by the user. The Publish UI uses this so a brand-new
// project doesn't get a Native-build row sourced from a 2-week-old food-photo
// app the same user happened to build. The user-ownership join is preserved
// so a forged project_id query can't read someone else's tasks.
func (db *Database) GetAppBuildTasksByUserAndAppProject(user *models.User, appProjectID string) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).
		Join("JOIN app_projects ap ON ap.id = app_build_task.app_project_id").
		Where("ap.user_id = ?", user.ID.String()).
		Where("app_build_task.app_project_id = ?", appProjectID).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return appBuildTasks, nil
}

// GetPendingAppBuildTasksByAppProject returns unfinished build tasks scoped
// by the AppProject FK. The Expo mobile path uses this to gate concurrent
// build attempts on the same AppProject.
func (db *Database) GetPendingAppBuildTasksByAppProject(appProjectID string) ([]models.AppBuildTask, error) {
	var tasks []models.AppBuildTask
	err := db.DB.NewSelect().
		Model(&tasks).
		Where("app_project_id = ?", appProjectID).
		Where("platform IN (?)", bun.In([]string{"Android", "IOS"})).
		Where("finished = ?", false).
		Scan(context.Background())
	return tasks, err
}

func (db *Database) GetAppBuildTaskById(id string) (*models.AppBuildTask, error) {
	var appBuildTask models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTask).Where("id = ?", id).Scan(context.Background())

	return &appBuildTask, err
}

func (db *Database) GetAppBuildTaskWithRelations(id string) (*models.AppBuildTask, error) {
	var appBuildTask models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTask).
		Relation("Build").
		Relation("AppProject").
		Relation("Annotation").
		Where("app_build_task.id = ?", id).
		Scan(context.Background())

	return &appBuildTask, err
}

// GetLatestSuccessfulBuildTaskByAppProjectAndPlatform returns the most
// recent finished+Built AppBuildTask for an AppProject + platform.
// Returns sql.ErrNoRows if no such task exists.
//
// Replaces GetLatestSuccessfulBuildTaskByProjectAndPlatform (which joined
// through the v1 shadow Project) — mobile builds now carry app_project_id
// directly. Platform values are "Android" or "IOS" (matches
// AppBuildTask.Platform vocabulary). The semantic gate is `finished = true`
// AND status_text = "Built"; "Succeeded" rows from the historical v1 path
// are NOT matched (they don't exist on the AppProject path).
func (db *Database) GetLatestSuccessfulBuildTaskByAppProjectAndPlatform(
	ctx context.Context,
	appProjectID uuid.UUID,
	platform string,
) (*models.AppBuildTask, error) {
	var task models.AppBuildTask
	err := db.DB.NewSelect().
		Model(&task).
		Where("app_project_id = ?", appProjectID).
		Where("platform = ?", platform).
		Where("status_text = ?", "Built").
		Where("finished = ?", true).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (db *Database) UpdateAppBuildTask(buildTask *models.AppBuildTask) error {
	_, err := db.DB.NewUpdate().Model(buildTask).Where("id = ?", buildTask.ID.String()).Exec(context.Background())
	return err
}

// ApplyExpoBuildStatusUpdate updates a single app_build_task row's status
// fields from a webhook event. It is intentionally column-constrained — Bun's
// `NewUpdate().Model(task)` would write ALL columns and silently null fields
// like ProjectID/CreatedAt if the caller built a fresh struct (see memory
// `feedback_bun_whole_row_update.md`). Only the four fields touched here are
// written.
//
// Idempotency: rows with finished=true are NOT updated — late or duplicate
// webhook deliveries cannot regress a terminal state.
//
// finished=false → progress event, only status_text changes.
// finished=true  → terminal event; status_text + error_message + annotation
//                  are set in the same UPDATE.
//
// Returns the number of rows updated (0 means: row was already finished and
// the update was skipped, or the task_id did not match any row).
func (db *Database) ApplyExpoBuildStatusUpdate(
	ctx context.Context,
	taskID uuid.UUID,
	statusText string,
	finished bool,
	errorMessage string,
	annotationID *uuid.UUID,
) (int64, error) {
	q := db.DB.NewUpdate().
		TableExpr("app_build_tasks").
		Set("status_text = ?", statusText).
		Set("updated_at = current_timestamp").
		Where("id = ?", taskID).
		Where("finished = false").
		Where("deleted_at IS NULL")

	if finished {
		q = q.Set("finished = ?", true).
			Set("error_message = ?", errorMessage)
		if annotationID != nil {
			q = q.Set("annotation_id = ?", *annotationID)
		}
	}

	res, err := q.Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// MarkOrphanedBuildTasksFailed terminally fails any unfinished
// app_build_tasks older than staleAfter. The cron caller
// (ReapStuckBuildTasksTask) runs this on the assumption that a Codemagic
// workflow that hasn't reported within the threshold isn't going to.
//
// Status vocabulary: writes the canonical "Failed" terminal value (P0 #2
// unification, 2026-05-05) — was previously lowercase "error" which left
// the cron-killed rows inconsistent with both the webhook-failure path
// (P0 #1 writes "Failed") and the legacy v1 path. The column-constrained
// UPDATE pattern (TableExpr + Set, no whole-row Model() call) is preserved
// to avoid the Bun whole-row footgun (memory feedback_bun_whole_row_update.md).
func (db *Database) MarkOrphanedBuildTasksFailed(ctx context.Context, staleAfter time.Duration) (int64, error) {
	res, err := db.DB.NewUpdate().
		TableExpr("app_build_tasks").
		Set("finished = ?", true).
		Set("status_text = ?", "Failed").
		Set("error_message = ?", "webhook timeout: build did not report within deadline").
		Set("updated_at = current_timestamp").
		Where("finished = false").
		Where("created_at < ?", time.Now().Add(-staleAfter)).
		Where("deleted_at IS NULL").
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (db *Database) CancelAppBuildTask(taskID string) error {
	_, err := db.DB.NewUpdate().
		TableExpr("app_build_tasks").
		Set("finished = ?", true).
		Set("status_text = ?", "Cancelled").
		Set("updated_at = current_timestamp").
		Where("id = ?", taskID).
		Where("finished = false").
		Exec(context.Background())
	return err
}

func (db *Database) GetIOSSignTaskById(id string) (*models.SignIPATask, error) {
	var signTask models.SignIPATask
	err := db.DB.NewSelect().Model(&signTask).Where("id = ?", id).Scan(context.Background())

	return &signTask, err
}

func (db *Database) CreateIOSSignTask(signTask models.SignIPATask) (*models.SignIPATask, error) {
	_, err := db.DB.NewInsert().Model(&signTask).Exec(context.Background())

	return &signTask, err
}

func (db *Database) UpdateIOSSignTask(signTask *models.SignIPATask) error {
	_, err := db.DB.NewUpdate().Model(signTask).Where("id = ?", signTask.ID.String()).Exec(context.Background())
	return err
}

// GetLatestIOSSignTaskByBuild returns the most recent sign task for a given
// build, or sql.ErrNoRows if none has been run. Used by the build card UI
// to display signing state after a page refresh.
func (db *Database) GetLatestIOSSignTaskByBuild(buildID string) (*models.SignIPATask, error) {
	var signTask models.SignIPATask
	err := db.DB.NewSelect().
		Model(&signTask).
		Where("build_id = ?", buildID).
		Order("created_at DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &signTask, nil
}
