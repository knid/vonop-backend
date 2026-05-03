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
		Set("permissions = EXCLUDED.permissions").
		Set("url_scheme = EXCLUDED.url_scheme").
		Set("universal_link_domain = EXCLUDED.universal_link_domain").
		Set("offline_enabled = EXCLUDED.offline_enabled").
		Set("caching_strategy = EXCLUDED.caching_strategy").
		Set("biometric_enabled = EXCLUDED.biometric_enabled").
		Set("dark_mode_enabled = EXCLUDED.dark_mode_enabled").
		Set("rating_enabled = EXCLUDED.rating_enabled").
		Set("rating_sessions = EXCLUDED.rating_sessions").
		Set("update_check_enabled = EXCLUDED.update_check_enabled").
		Set("app_store_url = EXCLUDED.app_store_url").
		Set("play_store_url = EXCLUDED.play_store_url").
		Set("landing_page_published = EXCLUDED.landing_page_published").
		Set("updated_at = now()").
		Returning("*").
		Exec(context.Background())
	return config, err
}
