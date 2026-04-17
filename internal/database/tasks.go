package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateAppBuildTask(build models.Build, platform, status string, autoDeploy bool) (*models.AppBuildTask, error) {
	appBuildTask := models.AppBuildTask{BuildID: build.ID, ProjectID: build.ProjectID, Platform: platform, StatusText: status, AutoDeploy: autoDeploy}
	_, err := db.DB.NewInsert().Model(&appBuildTask).Exec(context.Background())

	return &appBuildTask, err
}

func (db *Database) GetAppBuildTasksByUser(user *models.User) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).
		Join("JOIN projects ON projects.id = app_build_task.project_id").
		Where("projects.user_id = ?", user.ID.String()).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return appBuildTasks, nil
}

func (db *Database) GetPendingAppBuildTasks(project *models.Project) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).
		Where("project_id = ?", project.ID.String()).
		Where("platform IN (?)", bun.In([]string{"Android", "IOS"})).
		Where("finished = false").
		Scan(context.Background())

	return appBuildTasks, err
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
		Relation("Build.Project").
		Relation("Project").
		Relation("Project.User").
		Where("app_build_task.id = ?", id).
		Scan(context.Background())

	return &appBuildTask, err
}

func (db *Database) GetAppBuildTaskByProject(project *models.Project) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).Where("project_id = ?", project.ID).Scan(context.Background())

	return appBuildTasks, err
}

func (db *Database) UpdateAppBuildTask(buildTask *models.AppBuildTask) error {
	_, err := db.DB.NewUpdate().Model(buildTask).Where("id = ?", buildTask.ID.String()).Exec(context.Background())
	return err
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
