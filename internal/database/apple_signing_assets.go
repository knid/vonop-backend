package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// GetAppleSigningAssets returns the per-project apple_signing_assets row,
// or (nil, sql.ErrNoRows) when none exists. Soft-deleted rows are filtered
// by Bun's built-in deleted_at handling.
func (db *Database) GetAppleSigningAssets(ctx context.Context, appProjectID uuid.UUID) (*models.AppleSigningAssets, error) {
	var a models.AppleSigningAssets
	err := db.DB.NewSelect().
		Model(&a).
		Where("app_project_id = ?", appProjectID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// IsErrNoAppleSigningAssets reports whether err means "no row found".
// Used by controllers to decide whether to lazily generate one.
func IsErrNoAppleSigningAssets(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// InsertAppleSigningAssets persists a new row. The unique constraint on
// app_project_id makes concurrent first-generates collide here; the caller
// (EnsureAppleSigningAssets) treats the constraint violation as "another
// goroutine got there first; re-read the winning row".
func (db *Database) InsertAppleSigningAssets(ctx context.Context, a *models.AppleSigningAssets) error {
	_, err := db.DB.NewInsert().Model(a).Exec(ctx)
	return err
}

// UpsertAppleSigningAssets inserts or replaces the row keyed on
// app_project_id. Resets deleted_at to NULL on conflict so a re-generated
// asset on a previously soft-deleted row becomes visible again.
func (db *Database) UpsertAppleSigningAssets(ctx context.Context, a *models.AppleSigningAssets) (*models.AppleSigningAssets, error) {
	_, err := db.DB.NewInsert().Model(a).
		On("CONFLICT (app_project_id) DO UPDATE").
		Set("apple_connection_id = EXCLUDED.apple_connection_id").
		Set("certificate_id = EXCLUDED.certificate_id").
		Set("certificate_cred_ref = EXCLUDED.certificate_cred_ref").
		Set("certificate_expires_at = EXCLUDED.certificate_expires_at").
		Set("profile_id = EXCLUDED.profile_id").
		Set("profile_cred_ref = EXCLUDED.profile_cred_ref").
		Set("profile_expires_at = EXCLUDED.profile_expires_at").
		Set("bundle_id = EXCLUDED.bundle_id").
		Set("updated_at = current_timestamp").
		Set("deleted_at = NULL").
		Exec(ctx)
	return a, err
}

// DeleteAppleSigningAssets soft-deletes the row for appProjectID.
func (db *Database) DeleteAppleSigningAssets(ctx context.Context, appProjectID uuid.UUID) error {
	var a models.AppleSigningAssets
	_, err := db.DB.NewDelete().
		Model(&a).
		Where("app_project_id = ?", appProjectID).
		Exec(ctx)
	return err
}

// ListExpiringAppleSigningAssets returns all rows whose cert OR profile
// expires within `withinDays` days. Used by the daily renewal cron.
func (db *Database) ListExpiringAppleSigningAssets(ctx context.Context, withinDays int) ([]*models.AppleSigningAssets, error) {
	var rows []*models.AppleSigningAssets
	err := db.DB.NewSelect().
		Model(&rows).
		Where("certificate_expires_at <= now() + (? * interval '1 day') OR profile_expires_at <= now() + (? * interval '1 day')", withinDays, withinDays).
		Scan(ctx)
	return rows, err
}
