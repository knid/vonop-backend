package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateWebhookEndpoint(endpoint *models.WebhookEndpoint) (*models.WebhookEndpoint, error) {
	_, err := db.DB.NewInsert().Model(endpoint).Exec(context.Background())
	return endpoint, err
}

func (db *Database) GetWebhookEndpointsByUserID(userID string) ([]models.WebhookEndpoint, error) {
	var endpoints []models.WebhookEndpoint
	err := db.DB.NewSelect().
		Model(&endpoints).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return endpoints, err
}

func (db *Database) GetWebhookEndpointByID(id string) (*models.WebhookEndpoint, error) {
	var endpoint models.WebhookEndpoint
	err := db.DB.NewSelect().
		Model(&endpoint).
		Where("id = ?", id).
		Scan(context.Background())
	return &endpoint, err
}

func (db *Database) UpdateWebhookEndpoint(endpoint *models.WebhookEndpoint) (*models.WebhookEndpoint, error) {
	endpoint.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(endpoint).
		Where("id = ?", endpoint.ID).
		Exec(context.Background())
	return endpoint, err
}

func (db *Database) DeleteWebhookEndpoint(endpoint *models.WebhookEndpoint) error {
	_, err := db.DB.NewDelete().
		Model(endpoint).
		Where("id = ?", endpoint.ID).
		Exec(context.Background())
	return err
}

// GetWebhookEndpointsForEvent returns all active endpoints for a user that listen for a given event.
func (db *Database) GetWebhookEndpointsForEvent(userID, event string) ([]models.WebhookEndpoint, error) {
	var endpoints []models.WebhookEndpoint
	err := db.DB.NewSelect().
		Model(&endpoints).
		Where("user_id = ?", userID).
		Where("active = true").
		Where("events LIKE ?", "%"+event+"%").
		Scan(context.Background())
	return endpoints, err
}
