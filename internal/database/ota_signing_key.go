package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// GetOTAUpdateSigningKey returns the signing-key row for appProjectID, or
// (nil, sql.ErrNoRows) when none exists. Callers translate sql.ErrNoRows
// into "lazy generate this key" (see EnsureOTAUpdateSigningKey).
func (db *Database) GetOTAUpdateSigningKey(ctx context.Context, appProjectID uuid.UUID) (*models.OTAUpdateSigningKey, error) {
	var k models.OTAUpdateSigningKey
	err := db.DB.NewSelect().
		Model(&k).
		Where("app_project_id = ?", appProjectID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// IsErrNoOTASigningKey reports whether err means "no signing key row found".
// Used by controllers to decide whether to lazily generate one.
func IsErrNoOTASigningKey(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// InsertOTAUpdateSigningKey persists a new signing-key row. The PK is
// app_project_id (one keypair per project), so concurrent first-publishes
// can collide here — the controller layer treats unique-constraint
// violations as "another goroutine got there first; re-read the row".
func (db *Database) InsertOTAUpdateSigningKey(ctx context.Context, k *models.OTAUpdateSigningKey) error {
	_, err := db.DB.NewInsert().Model(k).Exec(ctx)
	return err
}
