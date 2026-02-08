package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetStoreByAppProjectID(appProjectID string) (*models.Store, error) {
	var store models.Store
	err := db.DB.NewSelect().Model(&store).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	return &store, err
}

func (db *Database) GetStoreByID(id string) (*models.Store, error) {
	var store models.Store
	err := db.DB.NewSelect().Model(&store).Where("id = ?", id).Scan(context.Background())
	return &store, err
}

func (db *Database) CreateStore(s *models.Store) (*models.Store, error) {
	_, err := db.DB.NewInsert().Model(s).Exec(context.Background())
	return s, err
}

func (db *Database) UpdateStore(s *models.Store) (*models.Store, error) {
	_, err := db.DB.NewUpdate().Model(s).Where("id = ?", s.ID).Exec(context.Background())
	return s, err
}
