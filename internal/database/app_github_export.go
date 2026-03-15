package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAppGitHubExport(export *models.AppGitHubExport) (*models.AppGitHubExport, error) {
	_, err := db.DB.NewInsert().Model(export).Exec(context.Background())
	return export, err
}

func (db *Database) GetAppGitHubExportByProjectID(projectID string) (*models.AppGitHubExport, error) {
	var export models.AppGitHubExport
	err := db.DB.NewSelect().Model(&export).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &export, err
}

func (db *Database) DeleteAppGitHubExport(projectID string) error {
	_, err := db.DB.NewDelete().Model((*models.AppGitHubExport)(nil)).Where("app_project_id = ?", projectID).Exec(context.Background())
	return err
}

func (db *Database) UpdateAppGitHubExportLastPushed(projectID string) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AppGitHubExport)(nil)).
		Set("last_pushed_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("app_project_id = ?", projectID).
		Exec(context.Background())
	return err
}

func (db *Database) GetAppGitHubExportsByUserID(userID string) ([]models.AppGitHubExport, error) {
	var exports []models.AppGitHubExport
	err := db.DB.NewSelect().Model(&exports).Where("user_id = ?", userID).Scan(context.Background())
	return exports, err
}
