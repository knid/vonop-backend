package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// GetAndroidKeystoreByProject returns the keystore row for projectID, or
// (nil, sql.ErrNoRows) when none exists. Soft-deleted rows are excluded
// automatically by Bun.
func (db *Database) GetAndroidKeystoreByProject(ctx context.Context, projectID uuid.UUID) (*models.AndroidKeystore, error) {
	var k models.AndroidKeystore
	err := db.DB.NewSelect().Model(&k).Where("project_id = ?", projectID).Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// UpsertAndroidKeystore creates or updates the keystore row keyed on
// project_id (which has a unique constraint).
func (db *Database) UpsertAndroidKeystore(ctx context.Context, k *models.AndroidKeystore) (*models.AndroidKeystore, error) {
	_, err := db.DB.NewInsert().Model(k).
		On("CONFLICT (project_id) DO UPDATE").
		Set("keystore_cred_ref = EXCLUDED.keystore_cred_ref").
		Set("properties_cred_ref = EXCLUDED.properties_cred_ref").
		Set("updated_at = current_timestamp").
		Exec(ctx)
	return k, err
}
