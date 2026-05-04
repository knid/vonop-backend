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
//
// Deprecated (kept for callers that don't yet care about channels): the
// manifest endpoint now uses GetLatestForChannel; rollback/promote queries
// rely on (channel, is_active). This function intentionally keeps its old
// behavior (no channel filter, no is_active filter) so any pre-P2#22 caller
// observes the same rows it always did.
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

// GetLatestForChannel returns the most-recent OTAUpdate row for
// (appProjectID, runtimeVersion, platform, channel) where is_active=true,
// or sql.ErrNoRows when none exists. P2#22 — channel is exact-match,
// no glob, no fallback; production must stay sealed off from staging /
// preview publishes.
func (db *Database) GetLatestForChannel(
	ctx context.Context,
	appProjectID uuid.UUID,
	runtimeVersion, platform, channel string,
) (*models.OTAUpdate, error) {
	var u models.OTAUpdate
	err := db.DB.NewSelect().
		Model(&u).
		Where("app_project_id = ?", appProjectID).
		Where("runtime_version = ?", runtimeVersion).
		Where("platform = ?", platform).
		Where("channel = ?", channel).
		Where("is_active = ?", true).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetOTAUpdateByID returns the OTAUpdate row with the given primary-key id,
// or sql.ErrNoRows when not found. Used by the rollback/promote endpoints.
func (db *Database) GetOTAUpdateByID(ctx context.Context, id uuid.UUID) (*models.OTAUpdate, error) {
	var u models.OTAUpdate
	err := db.DB.NewSelect().
		Model(&u).
		Where("id = ?", id).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeactivateNewerOTAUpdates marks every OTAUpdate row for
// (appProjectID, channel, platform) created strictly after target.CreatedAt
// as is_active=false. Used by RollbackOTAUpdate to "rewind" the active
// pointer to an older publish.
//
// Returns the number of rows touched (n.b. this includes rows that were
// already inactive — Postgres's `UPDATE ... WHERE created_at > ?` doesn't
// distinguish, and we don't actually need it to).
func (db *Database) DeactivateNewerOTAUpdates(
	ctx context.Context,
	appProjectID uuid.UUID,
	platform, channel string,
	target *models.OTAUpdate,
) (int64, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.OTAUpdate)(nil)).
		Set("is_active = ?", false).
		Set("rolled_back_from_update_id = ?", target.ID).
		Where("app_project_id = ?", appProjectID).
		Where("platform = ?", platform).
		Where("channel = ?", channel).
		Where("created_at > ?", target.CreatedAt).
		Where("is_active = ?", true).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SetOTAUpdateActive flips is_active on a single OTAUpdate row. Used to
// re-activate the rollback target when it had been deactivated by a
// previous rollback. Uses an explicit Set() rather than whole-row update
// to avoid the bun footgun documented in feedback_bun_whole_row_update.md.
func (db *Database) SetOTAUpdateActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := db.DB.NewUpdate().
		Model((*models.OTAUpdate)(nil)).
		Set("is_active = ?", active).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// SetOTAUpdateRolloutPercentage updates rollout_percentage on a single row
// without touching anything else. Use this rather than NewUpdate().Model(c)
// to dodge the whole-row-update bun footgun.
func (db *Database) SetOTAUpdateRolloutPercentage(ctx context.Context, id uuid.UUID, pct int16) error {
	_, err := db.DB.NewUpdate().
		Model((*models.OTAUpdate)(nil)).
		Set("rollout_percentage = ?", pct).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// IsErrNoOTAUpdate reports whether err indicates "no OTA update found".
func IsErrNoOTAUpdate(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ListOldOTAUpdatesForRV returns OTAUpdate rows for (appProjectID, rv, platform)
// beyond the keepN most recent. Used by the size-cap pruning at publish time.
// Returns rows ordered oldest-first so callers can delete in chronological
// order.
//
// Pre-P2#22 — does NOT filter by channel. Kept for legacy callers; the
// publish-time pruner now uses ListOldOTAUpdatesForChannel so a "preview"
// publish doesn't accidentally prune older "production" rows.
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

// ListOldOTAUpdatesForChannel — same as ListOldOTAUpdatesForRV but scoped
// to a single channel. Used by the post-publish pruner so a busy "preview"
// channel doesn't prune older "production" rows that may still be the
// active one for production devices.
func (db *Database) ListOldOTAUpdatesForChannel(ctx context.Context, appProjectID uuid.UUID, runtimeVersion, platform, channel string, keepN int) ([]models.OTAUpdate, error) {
	if keepN < 1 {
		keepN = 1
	}
	if channel == "" {
		channel = "production"
	}
	var rows []models.OTAUpdate
	err := db.DB.NewSelect().
		Model(&rows).
		Where("app_project_id = ?", appProjectID).
		Where("runtime_version = ?", runtimeVersion).
		Where("platform = ?", platform).
		Where("channel = ?", channel).
		Where("id NOT IN (?)",
			db.DB.NewSelect().
				Model((*models.OTAUpdate)(nil)).
				Column("id").
				Where("app_project_id = ?", appProjectID).
				Where("runtime_version = ?", runtimeVersion).
				Where("platform = ?", platform).
				Where("channel = ?", channel).
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
