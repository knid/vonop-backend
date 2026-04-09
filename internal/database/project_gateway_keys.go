package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateProjectGatewayKey(key *models.ProjectGatewayKey) (*models.ProjectGatewayKey, error) {
	_, err := db.DB.NewInsert().Model(key).Exec(context.Background())
	return key, err
}

func (db *Database) GetProjectGatewayKeyByHash(keyHash string) (*models.ProjectGatewayKey, error) {
	var key models.ProjectGatewayKey
	err := db.DB.NewSelect().
		Model(&key).
		Relation("User").
		Where("key_hash = ?", keyHash).
		Scan(context.Background())
	return &key, err
}

func (db *Database) GetProjectGatewayKeyByProjectID(projectID string) (*models.ProjectGatewayKey, error) {
	var key models.ProjectGatewayKey
	err := db.DB.NewSelect().
		Model(&key).
		Where("project_id = ?", projectID).
		Scan(context.Background())
	return &key, err
}

func (db *Database) SoftDeleteProjectGatewayKey(key *models.ProjectGatewayKey) error {
	key.DeletedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(key).
		Column("deleted_at").
		Where("id = ?", key.ID).
		Exec(context.Background())
	return err
}
