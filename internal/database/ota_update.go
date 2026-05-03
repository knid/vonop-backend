package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// InsertOTAUpdate persists a new OTA update row.
func (db *Database) InsertOTAUpdate(ctx context.Context, update *models.OTAUpdate) error {
	_, err := db.DB.NewInsert().Model(update).Exec(ctx)
	return err
}

// GetLatestOTAUpdate returns the most-recent OTAUpdate row for the given
// (appProjectID, runtimeVersion, platform) tuple, or sql.ErrNoRows when none
// exists. Callers translate sql.ErrNoRows to a 404 in the manifest endpoint.
func (db *Database) GetLatestOTAUpdate(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform string) (*models.OTAUpdate, error) {
	var u models.OTAUpdate
	err := db.DB.NewSelect().
		Model(&u).
		Where("app_project_id = ?", appProjectID).
		Where("runtime_version = ?", runtimeVersion).
		Where("platform = ?", platform).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// IsErrNoOTAUpdate reports whether err indicates "no OTA update found".
func IsErrNoOTAUpdate(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ListOldOTAUpdatesForRV returns OTAUpdate rows for (appProjectID, rv, platform)
// beyond the keepN most recent. Used by the size-cap pruning at publish time.
// Returns rows ordered oldest-first so callers can delete in chronological
// order.
func (db *Database) ListOldOTAUpdatesForRV(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform string, keepN int) ([]models.OTAUpdate, error) {
	if keepN < 1 {
		keepN = 1
	}
	// Subquery: ids of the keepN most recent updates for this RV/platform.
	// Outer query: everything else.
	var rows []models.OTAUpdate
	err := db.DB.NewSelect().
		Model(&rows).
		Where("app_project_id = ?", appProjectID).
		Where("runtime_version = ?", runtimeVersion).
		Where("platform = ?", platform).
		Where("id NOT IN (?)",
			db.DB.NewSelect().
				Model((*models.OTAUpdate)(nil)).
				Column("id").
				Where("app_project_id = ?", appProjectID).
				Where("runtime_version = ?", runtimeVersion).
				Where("platform = ?", platform).
				Order("created_at DESC").
				Limit(keepN),
		).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteOTAUpdate removes a single OTAUpdate row by ID.
func (db *Database) DeleteOTAUpdate(ctx context.Context, id uuid.UUID) error {
	_, err := db.DB.NewDelete().
		Model((*models.OTAUpdate)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}
