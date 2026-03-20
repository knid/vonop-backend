package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppIntegrationsByProjectID(projectID string) ([]models.AppIntegration, error) {
	var integrations []models.AppIntegration
	err := db.DB.NewSelect().
		Model(&integrations).
		Where("app_project_id = ?", projectID).
		Where("deleted_at IS NULL").
		Scan(context.Background())
	return integrations, err
}

func (db *Database) GetAppIntegrationByID(id string) (*models.AppIntegration, error) {
	var integration models.AppIntegration
	err := db.DB.NewSelect().
		Model(&integration).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Scan(context.Background())
	return &integration, err
}

func (db *Database) GetAppIntegrationByProvider(projectID string, provider string) (*models.AppIntegration, error) {
	var integration models.AppIntegration
	err := db.DB.NewSelect().
		Model(&integration).
		Where("app_project_id = ? AND provider = ?", projectID, provider).
		Where("deleted_at IS NULL").
		Scan(context.Background())
	return &integration, err
}

func (db *Database) CreateAppIntegration(i *models.AppIntegration) (*models.AppIntegration, error) {
	_, err := db.DB.NewInsert().Model(i).Exec(context.Background())
	return i, err
}

func (db *Database) UpdateAppIntegration(i *models.AppIntegration) (*models.AppIntegration, error) {
	_, err := db.DB.NewUpdate().Model(i).Where("id = ?", i.ID).Exec(context.Background())
	return i, err
}

// DeleteAppIntegration performs a soft delete by setting deleted_at.
func (db *Database) DeleteAppIntegration(i *models.AppIntegration) error {
	_, err := db.DB.NewDelete().Model(i).WherePK().Exec(context.Background())
	return err
}
