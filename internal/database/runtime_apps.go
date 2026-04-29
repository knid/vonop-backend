package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetRuntimeAppByAppProjectID(appProjectID string) (*models.RuntimeApp, error) {
	var ra models.RuntimeApp
	err := db.DB.NewSelect().Model(&ra).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &ra, nil
}

func (db *Database) CreateRuntimeApp(ra *models.RuntimeApp) (*models.RuntimeApp, error) {
	_, err := db.DB.NewInsert().Model(ra).Exec(context.Background())
	return ra, err
}

func (db *Database) UpdateRuntimeApp(ra *models.RuntimeApp) (*models.RuntimeApp, error) {
	ra.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(ra).WherePK().Exec(context.Background())
	return ra, err
}

func (db *Database) DeleteRuntimeApp(ra *models.RuntimeApp) error {
	_, err := db.DB.NewDelete().Model(ra).Where("id = ?", ra.ID).Exec(context.Background())
	return err
}

func (db *Database) GetRuntimeAppByFlyAppName(flyAppName string) (*models.RuntimeApp, error) {
	var ra models.RuntimeApp
	err := db.DB.NewSelect().Model(&ra).Where("fly_app_name = ?", flyAppName).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &ra, nil
}
