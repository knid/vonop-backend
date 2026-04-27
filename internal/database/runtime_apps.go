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

func (db *Database) GetRuntimeAppByCoolifyAppUUID(coolifyAppUUID string) (*models.RuntimeApp, error) {
	var ra models.RuntimeApp
	err := db.DB.NewSelect().Model(&ra).Where("coolify_app_uuid = ?", coolifyAppUUID).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &ra, nil
}

func (db *Database) DeleteRuntimeApp(ra *models.RuntimeApp) error {
	_, err := db.DB.NewDelete().Model(ra).Where("id = ?", ra.ID).Exec(context.Background())
	return err
}

// GetAllRuntimeAppsOnCoolify returns rows that still hold a coolify_app_uuid.
// Used by the migration script to find apps that haven't been moved to Fly yet.
func (db *Database) GetAllRuntimeAppsOnCoolify() ([]models.RuntimeApp, error) {
	var rows []models.RuntimeApp
	err := db.DB.NewSelect().Model(&rows).
		Where("coolify_app_uuid != ''").
		Order("created_at ASC").
		Scan(context.Background())
	return rows, err
}

// ClearCoolifyAppUUID is called after a successful migration to mark an app
// as fully on Fly. The column itself is dropped post-cutover.
func (db *Database) ClearCoolifyAppUUID(runtimeAppID string) error {
	_, err := db.DB.NewUpdate().Model((*models.RuntimeApp)(nil)).
		Set("coolify_app_uuid = ?", "").
		Where("id = ?", runtimeAppID).
		Exec(context.Background())
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
