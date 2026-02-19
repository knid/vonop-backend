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

func (db *Database) DeleteAppSnapshot(id string) error {
	_, err := db.DB.NewDelete().Model((*models.AppSnapshot)(nil)).Where("id = ?", id).Exec(context.Background())
	return err
}

func (db *Database) DeleteOldAutoSnapshots(projectID string, keepCount int) error {
	// Delete auto-snapshots beyond the keepCount most recent ones
	_, err := db.DB.NewRaw(`
		DELETE FROM app_snapshots
		WHERE id IN (
			SELECT id FROM app_snapshots
			WHERE app_project_id = ? AND source = 'auto'
			ORDER BY created_at DESC
			OFFSET ?
		)`, projectID, keepCount).Exec(context.Background())
	return err
}
