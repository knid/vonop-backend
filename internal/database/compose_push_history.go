package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// InsertComposePushHistory writes one audit-log row.
func (db *Database) InsertComposePushHistory(h *models.ComposePushHistory) error {
	_, err := db.DB.NewInsert().Model(h).Exec(context.Background())
	return err
}

// ListComposePushHistory returns the most-recent first, paginated history for
// an app project. Returns the slice + total count (for paginators). Limit is
// clamped to (0, 100]; offset clamped to [0, ∞).
func (db *Database) ListComposePushHistory(appProjectID string, limit, offset int) ([]models.ComposePushHistory, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var items []models.ComposePushHistory
	if err := db.DB.NewSelect().Model(&items).
		Where("app_project_id = ?", appProjectID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(context.Background()); err != nil {
		return nil, 0, err
	}

	count, err := db.DB.NewSelect().Model((*models.ComposePushHistory)(nil)).
		Where("app_project_id = ?", appProjectID).
		Count(context.Background())
	if err != nil {
		return nil, 0, err
	}

	return items, count, nil
}
