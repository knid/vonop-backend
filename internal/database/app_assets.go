package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppAssetsByProjectID(projectID string) ([]models.AppAsset, error) {
	var assets []models.AppAsset
	err := db.DB.NewSelect().Model(&assets).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return assets, err
}

func (db *Database) GetAppAssetByID(id string) (*models.AppAsset, error) {
	asset := new(models.AppAsset)
	err := db.DB.NewSelect().Model(asset).Where("id = ?", id).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (db *Database) CreateAppAsset(a *models.AppAsset) (*models.AppAsset, error) {
	_, err := db.DB.NewInsert().Model(a).Exec(context.Background())
	return a, err
}

func (db *Database) UpdateAppAsset(a *models.AppAsset) (*models.AppAsset, error) {
	_, err := db.DB.NewUpdate().Model(a).
		Column("file_name", "updated_at").
		Where("id = ?", a.ID).
		Exec(context.Background())
	return a, err
}

func (db *Database) DeleteAppAsset(a *models.AppAsset) error {
	_, err := db.DB.NewDelete().Model(a).Where("id = ?", a.ID).Exec(context.Background())
	return err
}
