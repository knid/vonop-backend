package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppEnvVarsByProjectID(projectID string) ([]models.AppEnvVar, error) {
	var envVars []models.AppEnvVar
	err := db.DB.NewSelect().Model(&envVars).Where("app_project_id = ?", projectID).Order("key ASC").Scan(context.Background())
	return envVars, err
}

func (db *Database) GetAppEnvVarByID(id string) (*models.AppEnvVar, error) {
	envVar := new(models.AppEnvVar)
	err := db.DB.NewSelect().Model(envVar).Where("id = ?", id).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return envVar, nil
}

func (db *Database) GetAppEnvVarByKey(projectID string, key string) (*models.AppEnvVar, error) {
	envVar := new(models.AppEnvVar)
	err := db.DB.NewSelect().Model(envVar).Where("app_project_id = ? AND key = ?", projectID, key).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return envVar, nil
}

func (db *Database) CreateAppEnvVar(e *models.AppEnvVar) (*models.AppEnvVar, error) {
	_, err := db.DB.NewInsert().Model(e).Exec(context.Background())
	return e, err
}

func (db *Database) UpdateAppEnvVar(e *models.AppEnvVar) (*models.AppEnvVar, error) {
	_, err := db.DB.NewUpdate().Model(e).
		Column("value", "is_public", "updated_at").
		Where("id = ?", e.ID).
		Exec(context.Background())
	return e, err
}

func (db *Database) DeleteAppEnvVar(e *models.AppEnvVar) error {
	_, err := db.DB.NewDelete().Model(e).Where("id = ?", e.ID).Exec(context.Background())
	return err
}
