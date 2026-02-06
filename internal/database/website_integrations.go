package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// WebsiteIntegration

func (db *Database) GetIntegrationsByWebsiteId(websiteId string) ([]models.WebsiteIntegration, error) {
	var integrations []models.WebsiteIntegration
	err := db.DB.NewSelect().Model(&integrations).Where("website_id = ?", websiteId).Scan(context.Background())
	return integrations, err
}

func (db *Database) GetIntegrationById(id string) (*models.WebsiteIntegration, error) {
	var integration models.WebsiteIntegration
	err := db.DB.NewSelect().Model(&integration).Where("id = ?", id).Scan(context.Background())
	return &integration, err
}

func (db *Database) GetIntegrationByWebsiteIdAndProvider(websiteId string, provider string) (*models.WebsiteIntegration, error) {
	var integration models.WebsiteIntegration
	err := db.DB.NewSelect().Model(&integration).Where("website_id = ? AND provider = ?", websiteId, provider).Scan(context.Background())
	return &integration, err
}

func (db *Database) CreateIntegration(i *models.WebsiteIntegration) (*models.WebsiteIntegration, error) {
	_, err := db.DB.NewInsert().Model(i).Exec(context.Background())
	return i, err
}

func (db *Database) UpdateIntegration(i *models.WebsiteIntegration) (*models.WebsiteIntegration, error) {
	_, err := db.DB.NewUpdate().Model(i).Where("id = ?", i.ID).Exec(context.Background())
	return i, err
}

func (db *Database) DeleteIntegration(i *models.WebsiteIntegration) error {
	_, err := db.DB.NewDelete().Model(i).Where("id = ?", i.ID).Exec(context.Background())
	return err
}
