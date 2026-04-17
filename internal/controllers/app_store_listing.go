package controllers

import (
	"database/sql"
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// GetOrCreateAppStoreListing returns the current listing for a project,
// or a fresh unsaved zero-value listing with sensible defaults so the
// frontend can render an empty form.
func (c *Controller) GetOrCreateAppStoreListing(project *models.AppProject) (*models.AppStoreListing, error) {
	listing, err := c.DB.GetAppStoreListing(project.ID.String())
	if err == nil {
		return listing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Return a default shell — not persisted until the user saves.
	return &models.AppStoreListing{
		AppProjectID: project.ID,
		Title:        project.Name,
		Locale:       "en-US",
		Screenshots:  []models.AppStoreListingScreenshot{},
	}, nil
}

// UpdateAppStoreListing applies changes from the wizard's auto-save. Upserts
// so the first save creates the row.
func (c *Controller) UpdateAppStoreListing(
	project *models.AppProject,
	req requests.UpdateAppStoreListingRequest,
) (*models.AppStoreListing, error) {
	// Load or init
	listing, err := c.GetOrCreateAppStoreListing(project)
	if err != nil {
		return nil, err
	}

	// Partial update — only fields present in the request. Empty strings
	// are valid clears (e.g. user wipes description) so we apply them.
	if req.IconS3Key != nil {
		listing.IconS3Key = *req.IconS3Key
	}
	if req.Title != nil {
		listing.Title = *req.Title
	}
	if req.Description != nil {
		listing.Description = *req.Description
	}
	if req.Locale != nil && *req.Locale != "" {
		listing.Locale = *req.Locale
	}
	if req.Screenshots != nil {
		// Replace in full — the client sends the entire ordered array.
		listing.Screenshots = make([]models.AppStoreListingScreenshot, 0, len(*req.Screenshots))
		for _, s := range *req.Screenshots {
			listing.Screenshots = append(listing.Screenshots, models.AppStoreListingScreenshot{
				ID:          s.ID,
				CanvasState: s.CanvasState,
			})
		}
	}
	if req.PrivacyPolicyURL != nil {
		listing.PrivacyPolicyURL = *req.PrivacyPolicyURL
	}

	saved, err := c.DB.UpsertAppStoreListing(listing)
	if err != nil {
		return nil, err
	}
	return saved, nil
}
