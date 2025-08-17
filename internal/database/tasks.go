package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAppNotificationTask(notification models.Notification) (*models.AppNotificationTask, error) {
	appNotificationTask := models.AppNotificationTask{Notification: notification}
	_, err := db.DB.NewInsert().Model(&appNotificationTask).Exec(context.Background())

	return &appNotificationTask, err
}

func (db *Database) GetAppNotificationTasks(notification *models.Notification) ([]models.AppNotificationTask, error) {
	var appNotificationTasks []models.AppNotificationTask
	err := db.DB.NewSelect().Model(&appNotificationTasks).Where("notification_id = ?", notification.ID.String()).Scan(context.Background())

	return appNotificationTasks, err
}

func (db *Database) GetAppNotificationTaskById(id string) (*models.AppNotificationTask, error) {
	appNotificationTask := models.AppNotificationTask{ID: uuid.MustParse(id)}
	err := db.DB.NewSelect().Model(&appNotificationTask).WherePK().Scan(context.Background())

	return &appNotificationTask, err
}

func (db *Database) CreateAppBuildTask(build models.Build) (*models.AppBuildTask, error) {
	appBuildTask := models.AppBuildTask{Build: build}
	_, err := db.DB.NewInsert().Model(&appBuildTask).Exec(context.Background())

	return &appBuildTask, err
}

func (db *Database) GetAppBuildTasks(build models.Build) ([]models.AppBuildTask, error) {
	var appBuildTasks []models.AppBuildTask
	err := db.DB.NewSelect().Model(&appBuildTasks).Where("build_id = ?", build.ID.String()).Scan(context.Background())

	return appBuildTasks, err
}

func (db *Database) GetAppBuildTaskById(id string) (*models.AppBuildTask, error) {
	appBuildTask := models.AppBuildTask{ID: uuid.MustParse(id)}
	err := db.DB.NewSelect().Model(&appBuildTask).WherePK().Scan(context.Background())

	return &appBuildTask, err
}

func (db *Database) CreateAppReleaseTask(release models.Release) (*models.AppReleaseTask, error) {
	appReleaseTask := models.AppReleaseTask{Release: release}
	_, err := db.DB.NewInsert().Model(&appReleaseTask).Exec(context.Background())

	return &appReleaseTask, err
}

func (db *Database) GetAppReleaseTasks(release models.Release) ([]models.AppReleaseTask, error) {
	var appReleaseTasks []models.AppReleaseTask
	err := db.DB.NewSelect().Model(&appReleaseTasks).Where("release_id = ?", release.ID.String()).Scan(context.Background())

	return appReleaseTasks, err
}

func (db *Database) GetAppReleaseTaskById(id string) (*models.AppReleaseTask, error) {
	appReleaseTask := models.AppReleaseTask{ID: uuid.MustParse(id)}
	err := db.DB.NewSelect().Model(&appReleaseTask).WherePK().Scan(context.Background())

	return &appReleaseTask, err
}
