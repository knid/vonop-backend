package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAndroidStoreAccess(access *models.AndroidStoreAccess) (*models.AndroidStoreAccess, error) {
	_, err := db.DB.NewInsert().Model(access).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return access, nil
}

func (db *Database) GetAndroidStoreAccessById(accessId string) (*models.AndroidStoreAccess, error) {
	var access models.AndroidStoreAccess

	err := db.DB.NewSelect().Model(&access).Where("id = ?", accessId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return &access, nil
}

func (db *Database) GetAndroidStoreAccessByUser(userId string) ([]models.AndroidStoreAccess, error) {
	var access []models.AndroidStoreAccess

	err := db.DB.NewSelect().Model(&access).Where("user_id = ?", userId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return access, nil
}

func (db *Database) DeleteAndroidStoreAccess(access *models.AndroidStoreAccess) error {
	_, err := db.DB.NewDelete().Model(access).Where("id = ?", access.ID.String()).Exec(context.Background())

	return err
}

func (db *Database) CreateIOSStoreAccess(access *models.IOSStoreAccess) (*models.IOSStoreAccess, error) {
	_, err := db.DB.NewInsert().Model(access).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return access, nil
}

func (db *Database) GetIOSStoreAccessById(accessId string) (*models.IOSStoreAccess, error) {
	var access models.IOSStoreAccess

	err := db.DB.NewSelect().Model(&access).Where("id = ?", accessId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return &access, nil
}

func (db *Database) GetIOSStoreAccessByUser(userId string) ([]models.IOSStoreAccess, error) {
	var access []models.IOSStoreAccess

	err := db.DB.NewSelect().Model(&access).Where("user_id = ?", userId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return access, nil
}

func (db *Database) DeleteIOSStoreAccess(access *models.IOSStoreAccess) error {
	_, err := db.DB.NewDelete().Model(access).Where("id = ?", access.ID.String()).Exec(context.Background())

	return err
}
