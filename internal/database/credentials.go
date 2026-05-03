package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppleConnectionById(id string) (*models.AppleConnection, error) {
	var credentials models.AppleConnection
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetAppleConnectionsByUser(user models.User) ([]models.AppleConnection, error) {
	var credentials []models.AppleConnection
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) GetAppleConnectionsByOrgID(orgID string) ([]models.AppleConnection, error) {
	var credentials []models.AppleConnection
	err := db.DB.NewSelect().Model(&credentials).Where("organization_id = ?", orgID).Scan(context.Background())

	return credentials, err
}

// GetAppleConnectionByOrg fetches a single Apple connection verified to belong
// to the given org. Returns (nil, nil) when not found.
func (db *Database) GetAppleConnectionByOrg(ctx context.Context, orgID, id uuid.UUID) (*models.AppleConnection, error) {
	var c models.AppleConnection
	err := db.DB.NewSelect().
		Model(&c).
		Where("id = ?", id).
		Where("organization_id = ?", orgID).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListAppleConnectionsByOrg returns all Apple connections owned by an org,
// newest first.
func (db *Database) ListAppleConnectionsByOrg(ctx context.Context, orgID uuid.UUID) ([]*models.AppleConnection, error) {
	var out []*models.AppleConnection
	err := db.DB.NewSelect().
		Model(&out).
		Where("organization_id = ?", orgID).
		Order("created_at DESC").
		Scan(ctx)
	return out, err
}

func (db *Database) CreateAppleConnection(credentials *models.AppleConnection) (*models.AppleConnection, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateAppleConnection(credentials *models.AppleConnection) (*models.AppleConnection, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteAppleConnection(credentials *models.AppleConnection) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}

func (db *Database) GetGooglePlayConnectionById(id string) (*models.GooglePlayConnection, error) {
	var credentials models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetGooglePlayConnectionsByUser(user models.User) ([]models.GooglePlayConnection, error) {
	var credentials []models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) GetGooglePlayConnectionsByOrgID(orgID string) ([]models.GooglePlayConnection, error) {
	var credentials []models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&credentials).Where("organization_id = ?", orgID).Scan(context.Background())

	return credentials, err
}

// GetGooglePlayConnectionByOrg fetches a single Google Play connection
// verified to belong to the given org. Returns (nil, nil) when not found.
func (db *Database) GetGooglePlayConnectionByOrg(ctx context.Context, orgID, id uuid.UUID) (*models.GooglePlayConnection, error) {
	var c models.GooglePlayConnection
	err := db.DB.NewSelect().
		Model(&c).
		Where("id = ?", id).
		Where("organization_id = ?", orgID).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListGooglePlayConnectionsByOrg returns all Google Play connections owned by
// an org, newest first.
func (db *Database) ListGooglePlayConnectionsByOrg(ctx context.Context, orgID uuid.UUID) ([]*models.GooglePlayConnection, error) {
	var out []*models.GooglePlayConnection
	err := db.DB.NewSelect().
		Model(&out).
		Where("organization_id = ?", orgID).
		Order("created_at DESC").
		Scan(ctx)
	return out, err
}

func (db *Database) CreateGooglePlayConnection(credentials *models.GooglePlayConnection) (*models.GooglePlayConnection, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateGooglePlayConnection(credentials *models.GooglePlayConnection) (*models.GooglePlayConnection, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteGooglePlayConnection(credentials *models.GooglePlayConnection) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}

// ListAllAppleConnections returns every non-deleted Apple connection across all
// orgs, ordered newest first. Used by the daily credential health cron.
func (db *Database) ListAllAppleConnections(ctx context.Context) ([]*models.AppleConnection, error) {
	var out []*models.AppleConnection
	err := db.DB.NewSelect().Model(&out).Order("created_at DESC").Scan(ctx)
	return out, err
}

// ListAllGooglePlayConnections returns every non-deleted Google Play connection
// across all orgs, ordered newest first. Used by the daily credential health cron.
func (db *Database) ListAllGooglePlayConnections(ctx context.Context) ([]*models.GooglePlayConnection, error) {
	var out []*models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&out).Order("created_at DESC").Scan(ctx)
	return out, err
}
