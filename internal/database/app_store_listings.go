package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/knid/wemob/internal/models"
)

// GetAppStoreListing returns the store listing for an AppProject. Returns
// sql.ErrNoRows when no listing exists yet — callers typically treat that
// as "create a fresh one on first update".
func (db *Database) GetAppStoreListing(appProjectID string) (*models.AppStoreListing, error) {
	listing := new(models.AppStoreListing)
	err := db.DB.NewSelect().
		Model(listing).
		Where("app_project_id = ?", appProjectID).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return listing, nil
}

// UpsertAppStoreListing creates the listing if none exists, otherwise
// updates the existing row in place. Used as the single write path from
// the wizard's auto-save so callers don't have to branch on existence.
func (db *Database) UpsertAppStoreListing(listing *models.AppStoreListing) (*models.AppStoreListing, error) {
	existing, err := db.GetAppStoreListing(listing.AppProjectID.String())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if existing == nil || errors.Is(err, sql.ErrNoRows) {
		_, err := db.DB.NewInsert().Model(listing).Exec(context.Background())
		if err != nil {
			return nil, err
		}
		return listing, nil
	}

	// Preserve immutable fields on update
	listing.ID = existing.ID
	listing.CreatedAt = existing.CreatedAt
	_, err = db.DB.NewUpdate().
		Model(listing).
		Where("id = ?", existing.ID.String()).
		Exec(context.Background())
	if err != nil {
		return nil, err
	}
	return listing, nil
}
