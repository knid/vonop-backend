package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppGeneratedFilesByProjectID(projectID string, platform string) ([]models.AppGeneratedFile, error) {
	var files []models.AppGeneratedFile
	err := db.DB.NewSelect().Model(&files).Where("app_project_id = ? AND platform = ?", projectID, platform).Order("file_path ASC").Scan(context.Background())
	return files, err
}

func (db *Database) GetAppGeneratedFileByID(id string) (*models.AppGeneratedFile, error) {
	var file models.AppGeneratedFile
	err := db.DB.NewSelect().Model(&file).Where("id = ?", id).Scan(context.Background())
	return &file, err
}

func (db *Database) GetAppGeneratedFileByPath(projectID string, platform string, filePath string) (*models.AppGeneratedFile, error) {
	var file models.AppGeneratedFile
	err := db.DB.NewSelect().Model(&file).Where("app_project_id = ? AND platform = ? AND file_path = ?", projectID, platform, filePath).Scan(context.Background())
	return &file, err
}

func (db *Database) CreateAppGeneratedFile(f *models.AppGeneratedFile) (*models.AppGeneratedFile, error) {
	_, err := db.DB.NewInsert().Model(f).Exec(context.Background())
	return f, err
}

func (db *Database) UpsertAppGeneratedFile(f *models.AppGeneratedFile) (*models.AppGeneratedFile, error) {
	_, err := db.DB.NewInsert().Model(f).
		On("CONFLICT (app_project_id, platform, file_path) DO UPDATE").
		Set("content = EXCLUDED.content").
		Set("file_type = EXCLUDED.file_type").
		Set("updated_at = current_timestamp").
		Exec(context.Background())
	return f, err
}

func (db *Database) BulkCreateAppGeneratedFiles(files []models.AppGeneratedFile) error {
	if len(files) == 0 {
		return nil
	}
	_, err := db.DB.NewInsert().Model(&files).Exec(context.Background())
	return err
}

func (db *Database) UpdateAppGeneratedFile(file *models.AppGeneratedFile) (*models.AppGeneratedFile, error) {
	_, err := db.DB.NewUpdate().Model(file).WherePK().Exec(context.Background())
	return file, err
}

func (db *Database) DeleteAppGeneratedFile(file *models.AppGeneratedFile) error {
	_, err := db.DB.NewDelete().Model(file).WherePK().Exec(context.Background())
	return err
}

func (db *Database) DeleteAppGeneratedFilesByProjectPlatform(projectID string, platform string) error {
	_, err := db.DB.NewDelete().Model((*models.AppGeneratedFile)(nil)).Where("app_project_id = ? AND platform = ?", projectID, platform).Exec(context.Background())
	return err
}
