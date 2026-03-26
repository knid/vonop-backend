package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppDeploymentsByProjectID(projectID string) ([]models.AppDeployment, error) {
	var deployments []models.AppDeployment
	err := db.DB.NewSelect().Model(&deployments).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return deployments, err
}

func (db *Database) GetAppDeploymentByID(id string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).Where("id = ?", id).Scan(context.Background())
	return &deployment, err
}

func (db *Database) GetLatestAppDeployment(projectID string, platform string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).Where("app_project_id = ? AND platform = ?", projectID, platform).Order("created_at DESC").Limit(1).Scan(context.Background())
	return &deployment, err
}

func (db *Database) CreateAppDeployment(d *models.AppDeployment) (*models.AppDeployment, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) GetBuildingAppDeployment(projectID string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).
		Where("app_project_id = ? AND platform = ? AND status IN (?)", projectID, "web", "building").
		Order("created_at DESC").Limit(1).Scan(context.Background())
	return &deployment, err
}

func (db *Database) UpdateAppDeployment(d *models.AppDeployment) (*models.AppDeployment, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}
