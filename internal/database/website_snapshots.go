package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateSnapshot(s *models.WebsiteSnapshot) (*models.WebsiteSnapshot, error) {
	_, err := db.DB.NewInsert().Model(s).Exec(context.Background())
	return s, err
}

func (db *Database) GetSnapshotsByWebsiteId(websiteId string) ([]models.WebsiteSnapshot, error) {
	var snapshots []models.WebsiteSnapshot
	err := db.DB.NewSelect().Model(&snapshots).Where("website_id = ?", websiteId).Order("created_at DESC").Scan(context.Background())
	return snapshots, err
}

func (db *Database) GetSnapshotById(id string) (*models.WebsiteSnapshot, error) {
	var snapshot models.WebsiteSnapshot
	err := db.DB.NewSelect().Model(&snapshot).Where("id = ?", id).Scan(context.Background())
	return &snapshot, err
}
