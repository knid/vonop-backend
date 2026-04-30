package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateAPIKey(key *models.APIKey) (*models.APIKey, error) {
	_, err := db.DB.NewInsert().Model(key).Exec(context.Background())
	return key, err
}

func (db *Database) GetAPIKeysByUserID(userID string) ([]models.APIKey, error) {
	var keys []models.APIKey
	err := db.DB.NewSelect().
		Model(&keys).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return keys, err
}

func (db *Database) GetAPIKeysByOrgID(orgID string) ([]models.APIKey, error) {
	var keys []models.APIKey
	err := db.DB.NewSelect().
		Model(&keys).
		Where("organization_id = ?", orgID).
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return keys, err
}

func (db *Database) GetAPIKeyByHash(keyHash string) (*models.APIKey, error) {
	var key models.APIKey
	err := db.DB.NewSelect().
		Model(&key).
		Relation("User").
		Where("key_hash = ?", keyHash).
		Scan(context.Background())
	return &key, err
}

func (db *Database) GetAPIKeyByID(id string) (*models.APIKey, error) {
	var key models.APIKey
	err := db.DB.NewSelect().
		Model(&key).
		Where("id = ?", id).
		Scan(context.Background())
	return &key, err
}

func (db *Database) UpdateAPIKeyLastUsed(key *models.APIKey) error {
	key.LastUsed = time.Now()
	_, err := db.DB.NewUpdate().
		Model(key).
		Column("last_used").
		Where("id = ?", key.ID).
		Exec(context.Background())
	return err
}

func (db *Database) DeleteAPIKey(key *models.APIKey) error {
	_, err := db.DB.NewDelete().
		Model(key).
		Where("id = ?", key.ID).
		Exec(context.Background())
	return err
}
