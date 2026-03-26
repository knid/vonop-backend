package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetCoolifyProjectByAppProjectID(appProjectID string) (*models.CoolifyProject, error) {
	var cp models.CoolifyProject
	err := db.DB.NewSelect().Model(&cp).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	return &cp, err
}

func (db *Database) CreateCoolifyProject(cp *models.CoolifyProject) (*models.CoolifyProject, error) {
	_, err := db.DB.NewInsert().Model(cp).Exec(context.Background())
	return cp, err
}

func (db *Database) UpdateCoolifyProject(cp *models.CoolifyProject) (*models.CoolifyProject, error) {
	_, err := db.DB.NewUpdate().Model(cp).Where("id = ?", cp.ID).Exec(context.Background())
	return cp, err
}

func (db *Database) GetCoolifyProjectByCoolifyAppUUID(coolifyAppUUID string) (*models.CoolifyProject, error) {
	var cp models.CoolifyProject
	err := db.DB.NewSelect().Model(&cp).Where("coolify_app_uuid = ?", coolifyAppUUID).Scan(context.Background())
	return &cp, err
}

func (db *Database) DeleteCoolifyProject(cp *models.CoolifyProject) error {
	_, err := db.DB.NewDelete().Model(cp).Where("id = ?", cp.ID).Exec(context.Background())
	return err
}
