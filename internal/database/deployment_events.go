package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AppendDeploymentEvent appends a new row to deployment_events.
// actorID is optional — pass nil for system-originated events.
// Metadata may be nil.
func AppendDeploymentEvent(
	ctx context.Context,
	db *bun.DB,
	deploymentID uuid.UUID,
	eventType, actorType string,
	actorID *uuid.UUID,
	metadata map[string]interface{},
) error {
	ev := models.DeploymentEvent{
		DeploymentID: deploymentID,
		EventType:    eventType,
		ActorType:    actorType,
		ActorID:      actorID,
		Metadata:     metadata,
		OccurredAt:   time.Now().UTC(),
	}
	_, err := db.NewInsert().Model(&ev).Exec(ctx)
	return err
}

// ListDeploymentEvents returns all events for a deployment, oldest first.
func ListDeploymentEvents(
	ctx context.Context,
	db *bun.DB,
	deploymentID uuid.UUID,
) ([]models.DeploymentEvent, error) {
	var events []models.DeploymentEvent
	err := db.NewSelect().
		Model(&events).
		Where("deployment_id = ?", deploymentID).
		Order("occurred_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// HasEvent reports whether a deployment already has at least one event
// of the given type. Used by the store-API poller for idempotent writes.
func HasEvent(ctx context.Context, db *bun.DB, deploymentID uuid.UUID, eventType string) (bool, error) {
	count, err := db.NewSelect().
		Model((*models.DeploymentEvent)(nil)).
		Where("deployment_id = ?", deploymentID).
		Where("event_type = ?", eventType).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
