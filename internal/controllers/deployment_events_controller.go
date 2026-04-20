package controllers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetDeploymentEvents(ctx context.Context, user *models.User, deploymentID uuid.UUID) (*responses.DeploymentEventsResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	var deployment models.Deployment
	if err := c.DB.DB.NewSelect().
		Model(&deployment).
		Where("id = ?", deploymentID).
		Scan(ctx); err != nil {
		return nil, err
	}
	var project models.Project
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", deployment.ProjectID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		return nil, errors.New("not authorized")
	}

	events, err := database.ListDeploymentEvents(ctx, c.DB.DB, deploymentID)
	if err != nil {
		return nil, err
	}

	items := make([]responses.DeploymentEventItem, 0, len(events))
	for _, ev := range events {
		item := responses.DeploymentEventItem{
			ID:         ev.ID.String(),
			EventType:  ev.EventType,
			ActorType:  ev.ActorType,
			OccurredAt: ev.OccurredAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if name, ok := ev.Metadata["reviewer_name"].(string); ok {
			item.ActorName = name
		}
		if note, ok := ev.Metadata["note"].(string); ok {
			item.Note = note
		}
		items = append(items, item)
	}
	return &responses.DeploymentEventsResponse{Items: items}, nil
}
