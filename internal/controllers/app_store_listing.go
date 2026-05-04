package controllers

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

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

// GetListingForSubmit loads the parent listing + every locale row for a
// project. Used by the submit pipeline to populate ExpoIOSDeployParams /
// ExpoAndroidDeployParams.Locales so per-locale store metadata reaches
// Apple / Google Play.
//
// Returns (nil, nil, sql.ErrNoRows) when the project has no listing yet —
// callers should treat that as a hard error (the wizard requires the
// listing be saved before submit).
//
// `locale` is currently unused — the entire localization set is returned
// so the submit step can write all per-locale metadata folders. Reserved
// for a future "submit only this locale" override.
func (c *Controller) GetListingForSubmit(ctx context.Context, projectID string, locale string) (*models.AppStoreListing, map[string]*models.AppStoreListingLocalization, error) {
	_ = locale // reserved for future per-locale submission filtering
	listing, err := c.DB.GetAppStoreListing(projectID)
	if err != nil {
		return nil, nil, err
	}
	locales, err := c.DB.GetLocalizationsByListing(ctx, listing.ID)
	if err != nil {
		return listing, nil, err
	}
	return listing, locales, nil
}

// UpsertListingLocalization writes one locale's per-locale copy +
// screenshots. Creates the row if it doesn't exist, otherwise updates in
// place. Caller is responsible for resolving listingID from the project
// (typically via GetOrCreateAppStoreListing → save → ID).
func (c *Controller) UpsertListingLocalization(ctx context.Context, listingID uuid.UUID, locale string, fields *models.AppStoreListingLocalization) (*models.AppStoreListingLocalization, error) {
	if fields == nil {
		return nil, errors.New("upsert localization: fields is required")
	}
	if locale == "" {
		return nil, errors.New("upsert localization: locale is required")
	}
	if listingID == uuid.Nil {
		return nil, errors.New("upsert localization: listingID is required")
	}
	fields.ListingID = listingID
	fields.Locale = locale
	saved, err := c.DB.UpsertLocalization(ctx, fields)
	if err != nil {
		return nil, err
	}
	return saved, nil
}
