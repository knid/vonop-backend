package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppStoreCredentialById(id string) (*models.AppStoreCredentials, error) {
	var credentials models.AppStoreCredentials
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetAppStoreCredentialsByUser(user models.User) ([]models.AppStoreCredentials, error) {
	var credentials []models.AppStoreCredentials
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) CreateAppStoreCredentials(credentials *models.AppStoreCredentials) (*models.AppStoreCredentials, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateAppStoreCredentials(credentials *models.AppStoreCredentials) (*models.AppStoreCredentials, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteAppStoreCredentials(credentials *models.AppStoreCredentials) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}

func (db *Database) GetGooglePlayCredentialById(id string) (*models.GooglePlayCredentials, error) {
	var credentials models.GooglePlayCredentials
	err := db.DB.NewSelect().Model(&credentials).Where("id = ?", id).Scan(context.Background())

	return &credentials, err
}

func (db *Database) GetGooglePlayCredentialsByUser(user models.User) ([]models.GooglePlayCredentials, error) {
	var credentials []models.GooglePlayCredentials
	err := db.DB.NewSelect().Model(&credentials).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return credentials, err
}

func (db *Database) CreateGooglePlayCredentials(credentials *models.GooglePlayCredentials) (*models.GooglePlayCredentials, error) {
	_, err := db.DB.NewInsert().Model(credentials).Exec(context.Background())

	return credentials, err
}

func (db *Database) UpdateGooglePlayCredentials(credentials *models.GooglePlayCredentials) (*models.GooglePlayCredentials, error) {
	_, err := db.DB.NewUpdate().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return credentials, err
}

func (db *Database) DeleteGooglePlayCredentials(credentials *models.GooglePlayCredentials) error {
	_, err := db.DB.NewDelete().Model(credentials).Where("id = ?", credentials.ID.String()).Exec(context.Background())

	return err
}
