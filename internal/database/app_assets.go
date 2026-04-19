package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
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

// GetAppAssetsByIDs loads a batch of AppAsset rows by primary key. Excludes
// soft-deleted rows (Bun's soft_delete tag on AppAsset.DeletedAt handles this
// automatically on NewSelect). Order of the returned slice is not guaranteed —
// callers that care should index by ID.
func (db *Database) GetAppAssetsByIDs(ids []string) ([]models.AppAsset, error) {
	var assets []models.AppAsset
	if len(ids) == 0 {
		return assets, nil
	}
	err := db.DB.NewSelect().
		Model(&assets).
		Where("id IN (?)", bun.In(ids)).
		Scan(context.Background())
	return assets, err
}

// GetRecentAppAssets returns the most recently created non-deleted AppAsset
// rows for a project, capped at limit. Used to build the agent's per-turn
// project-assets manifest. Bun's soft_delete tag on AppAsset.DeletedAt
// automatically excludes soft-deleted rows.
func (db *Database) GetRecentAppAssets(projectID uuid.UUID, limit int) ([]models.AppAsset, error) {
	var assets []models.AppAsset
	err := db.DB.NewSelect().
		Model(&assets).
		Where("app_project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return assets, err
}
