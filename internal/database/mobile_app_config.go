package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetMobileAppConfig(projectId string) (*models.MobileAppConfig, error) {
	var config models.MobileAppConfig
	err := db.DB.NewSelect().Model(&config).
		Where("project_id = ?", projectId).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (db *Database) UpsertMobileAppConfig(config *models.MobileAppConfig) (*models.MobileAppConfig, error) {
	_, err := db.DB.NewInsert().Model(config).
		On("CONFLICT (project_id) DO UPDATE").
		Set("push_enabled = EXCLUDED.push_enabled").
		Set("analytics_enabled = EXCLUDED.analytics_enabled").
		Set("pull_to_refresh_enabled = EXCLUDED.pull_to_refresh_enabled").
		Set("external_browser_links = EXCLUDED.external_browser_links").
		Set("bridge_enabled = EXCLUDED.bridge_enabled").
		Set("bridge_methods = EXCLUDED.bridge_methods").
		Set("updated_at = now()").
		Returning("*").
		Exec(context.Background())
	return config, err
}
