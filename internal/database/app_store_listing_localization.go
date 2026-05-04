package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// GetLocalizationsByListing returns every locale row for a listing, keyed
// by locale code (e.g. "en-US"). Returns an empty map (not nil) when the
// listing has no localizations yet so callers can safely range over it.
func (db *Database) GetLocalizationsByListing(ctx context.Context, listingID uuid.UUID) (map[string]*models.AppStoreListingLocalization, error) {
	var rows []models.AppStoreListingLocalization
	err := db.DB.NewSelect().
		Model(&rows).
		Where("listing_id = ?", listingID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*models.AppStoreListingLocalization, len(rows))
	for i := range rows {
		// Take address of the slice element directly so the map points
		// into the same backing array — safe because rows is local.
		row := rows[i]
		out[row.Locale] = &row
	}
	return out, nil
}

// UpsertLocalization creates or updates a single localization row keyed
// on (listing_id, locale). Resets deleted_at = NULL on conflict so a
// previously-soft-deleted locale comes back to life when the user re-adds
// it from the wizard.
//
// Caller is responsible for setting ListingID and Locale.
func (db *Database) UpsertLocalization(ctx context.Context, row *models.AppStoreListingLocalization) (*models.AppStoreListingLocalization, error) {
	_, err := db.DB.NewInsert().Model(row).
		On("CONFLICT (listing_id, locale) DO UPDATE").
		Set("title = EXCLUDED.title").
		Set("description = EXCLUDED.description").
		Set("release_notes = EXCLUDED.release_notes").
		Set("subtitle = EXCLUDED.subtitle").
		Set("promotional_text = EXCLUDED.promotional_text").
		Set("keywords = EXCLUDED.keywords").
		Set("short_description = EXCLUDED.short_description").
		Set("support_url = EXCLUDED.support_url").
		Set("marketing_url = EXCLUDED.marketing_url").
		Set("screenshots = EXCLUDED.screenshots").
		Set("updated_at = current_timestamp").
		Set("deleted_at = NULL").
		Exec(ctx)
	return row, err
}

// DeleteLocalization soft-deletes a single localization row by id.
func (db *Database) DeleteLocalization(ctx context.Context, id uuid.UUID) error {
	_, err := db.DB.NewDelete().
		Model((*models.AppStoreListingLocalization)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}
