package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// IsWebhookEventProcessed checks whether a Stripe webhook event has already been handled.
func (db *Database) IsWebhookEventProcessed(eventID string) (bool, error) {
	exists, err := db.DB.NewSelect().
		Model((*models.WebhookEvent)(nil)).
		Where("event_id = ?", eventID).
		Exists(context.Background())
	return exists, err
}

// MarkWebhookEventProcessed records a Stripe webhook event as processed.
func (db *Database) MarkWebhookEventProcessed(eventID string, eventType string) error {
	event := &models.WebhookEvent{
		EventID:   eventID,
		EventType: eventType,
	}
	_, err := db.DB.NewInsert().Model(event).Exec(context.Background())
	return err
}
