package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppSnapshotsByProjectID(projectID string) ([]models.AppSnapshot, error) {
	var snapshots []models.AppSnapshot
	err := db.DB.NewSelect().Model(&snapshots).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return snapshots, err
}

func (db *Database) GetAppSnapshotByID(id string) (*models.AppSnapshot, error) {
	var snapshot models.AppSnapshot
	err := db.DB.NewSelect().Model(&snapshot).Where("id = ?", id).Scan(context.Background())
	return &snapshot, err
}

func (db *Database) CreateAppSnapshot(s *models.AppSnapshot) (*models.AppSnapshot, error) {
	_, err := db.DB.NewInsert().Model(s).Exec(context.Background())
	return s, err
}
