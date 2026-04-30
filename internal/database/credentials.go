package database

import (
	"context"

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
