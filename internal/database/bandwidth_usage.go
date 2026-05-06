package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// GetBandwidthUsageForOrgPeriod returns the org's bandwidth row for the given
// billing period (or nil with no error when no row exists yet).
func (db *Database) GetBandwidthUsageForOrgPeriod(ctx context.Context, orgID uuid.UUID, periodStart time.Time) (*models.AppBandwidthUsage, error) {
	var row models.AppBandwidthUsage
	err := db.DB.NewSelect().
		Model(&row).
		Where("organization_id = ?", orgID).
		Where("period_start = ?", periodStart).
		Limit(1).
		Scan(ctx)
	if err != nil {
		// Caller treats sql.ErrNoRows as "no usage tracked yet, allow deploy".
		return nil, err
	}
	return &row, nil
}

// UpsertBandwidthUsage writes the cron-aggregated bytes_used for an org in a
// billing period. Idempotent: re-running with the same period overwrites bytes.
func (db *Database) UpsertBandwidthUsage(ctx context.Context, row *models.AppBandwidthUsage) error {
	row.UpdatedAt = time.Now()
	_, err := db.DB.NewInsert().
		Model(row).
		On("CONFLICT (organization_id, period_start) DO UPDATE").
		Set("bytes_used = EXCLUDED.bytes_used").
		Set("period_end = EXCLUDED.period_end").
		Set("last_synced_at = EXCLUDED.last_synced_at").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}
