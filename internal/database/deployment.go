package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetDeployments(user *models.User) ([]models.Deployment, error) {
	var deployments []models.Deployment
	err := db.DB.NewSelect().Model(&deployments).Where("user_id = ?", user.ID).Scan(context.Background(), &deployments)
	if err != nil {
		return nil, err
	}
	return deployments, nil
}

func (db *Database) GetDeploymentByID(deploymentID string) (*models.Deployment, error) {
	var deployment models.Deployment
	err := db.DB.NewSelect().Model(&deployment).Where("id = ?", deploymentID).Scan(context.Background(), &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (db *Database) GetDeploymentWithRelations(deploymentID string) (*models.Deployment, error) {
	var deployment models.Deployment
	err := db.DB.NewSelect().Model(&deployment).
		Relation("User").
		Where("deployment.id = ?", deploymentID).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (db *Database) CreateDeployment(deployment *models.Deployment) (*models.Deployment, error) {
	_, err := db.DB.NewInsert().Model(deployment).Exec(context.Background())
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

func (db *Database) UpdateDeployment(deployment *models.Deployment) (*models.Deployment, error) {
	_, err := db.DB.NewUpdate().Model(deployment).Where("id = ?", deployment.ID).Exec(context.Background())
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

