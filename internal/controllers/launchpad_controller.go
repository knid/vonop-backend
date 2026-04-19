package controllers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

// GetLaunchpad assembles the single aggregate response for the Ship
// tab's Launchpad surface. Callers supply the AppProject ID and the
// authenticated user; the controller enforces ownership before building
// the response.
//
// Returns (nil, error) when the app doesn't exist or the user doesn't
// own it; callers translate that to 404 or 403.
func (c *Controller) GetLaunchpad(ctx context.Context, user *models.User, appID uuid.UUID) (*responses.LaunchpadResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}
	// Load the project (and verify ownership).
	var project models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		// Organization-shared apps land here too; Phase 2a doesn't
		// cover org membership checks — that's Phase 4/5.
		return nil, errors.New("not authorized")
	}

	resp := &responses.LaunchpadResponse{
		App: responses.LaunchpadApp{
			ID:   project.ID.String(),
			Name: project.Name,
		},
	}

	// Phase 2a populates HeroStatus, WebPreview, Stores. Task 3 fills
	// the real values; this skeleton returns zero data so Task 5's
	// smoke test proves the route is wired before full logic lands.
	return resp, nil
}
