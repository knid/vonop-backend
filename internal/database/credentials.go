package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppStoreCredentialById(id string) (*models.AppleConnection, error) {
	var credentials models.AppleConnection
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetAppStoreCredentialsByUser(user models.User) ([]models.AppleConnection, error) {
	var credentials []models.AppleConnection
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) CreateAppStoreCredentials(credentials *models.AppleConnection) (*models.AppleConnection, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateAppStoreCredentials(credentials *models.AppleConnection) (*models.AppleConnection, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteAppStoreCredentials(credentials *models.AppleConnection) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}

func (db *Database) GetGooglePlayCredentialById(id string) (*models.GooglePlayConnection, error) {
	var credentials models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetGooglePlayCredentialsByUser(user models.User) ([]models.GooglePlayConnection, error) {
	var credentials []models.GooglePlayConnection
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) CreateGooglePlayCredentials(credentials *models.GooglePlayConnection) (*models.GooglePlayConnection, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateGooglePlayCredentials(credentials *models.GooglePlayConnection) (*models.GooglePlayConnection, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteGooglePlayCredentials(credentials *models.GooglePlayConnection) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}
