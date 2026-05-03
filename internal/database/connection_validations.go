package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// ConnectionValidationStore is the interface used by controllers + crons
// for appending and querying validation outcomes. Defined here at the
// database boundary so callers can fake it in tests.
type ConnectionValidationStore interface {
	AppendConnectionValidation(ctx context.Context, v *models.ConnectionValidation) error
	LatestConnectionValidation(ctx context.Context, kind string, id uuid.UUID) (*models.ConnectionValidation, error)
	ListConnectionValidations(ctx context.Context, kind string) ([]*models.ConnectionValidation, error)
}

// AppendConnectionValidation inserts a new validation row.
func (db *Database) AppendConnectionValidation(ctx context.Context, v *models.ConnectionValidation) error {
	_, err := db.DB.NewInsert().Model(v).Exec(ctx)
	return err
}

// LatestConnectionValidation returns the most recent validation for one
// (kind, connection) pair, or nil if none.
func (db *Database) LatestConnectionValidation(ctx context.Context, kind string, id uuid.UUID) (*models.ConnectionValidation, error) {
	var v models.ConnectionValidation
	err := db.DB.NewSelect().
		Model(&v).
		Where("connection_kind = ?", kind).
		Where("connection_id = ?", id).
		Order("validated_at DESC").
		Limit(1).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ListConnectionValidations returns all validation rows for one kind,
// newest first. Used by crons that walk all connections.
func (db *Database) ListConnectionValidations(ctx context.Context, kind string) ([]*models.ConnectionValidation, error) {
	var out []*models.ConnectionValidation
	err := db.DB.NewSelect().
		Model(&out).
		Where("connection_kind = ?", kind).
		Order("validated_at DESC").
		Scan(ctx)
	return out, err
}
