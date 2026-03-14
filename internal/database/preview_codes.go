package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreatePreviewCode(code *models.PreviewCode) (*models.PreviewCode, error) {
	_, err := db.DB.NewInsert().Model(code).Exec(context.Background())
	return code, err
}

func (db *Database) GetPreviewCodeByCode(code string) (*models.PreviewCode, error) {
	var pc models.PreviewCode
	err := db.DB.NewSelect().Model(&pc).Where("code = ?", code).Scan(context.Background())
	return &pc, err
}

func (db *Database) MarkPreviewCodeRedeemed(code string, redeemedBy string) error {
	_, err := db.DB.NewUpdate().Model((*models.PreviewCode)(nil)).
		Set("redeemed_at = ?", time.Now()).
		Set("redeemed_by = ?", redeemedBy).
		Where("code = ?", code).
		Where("redeemed_at IS NULL").
		Exec(context.Background())
	return err
}

func (db *Database) CleanupExpiredPreviewCodes() (int64, error) {
	res, err := db.DB.NewDelete().Model((*models.PreviewCode)(nil)).
		Where("expires_at < ?", time.Now().Add(-1*time.Hour)).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
