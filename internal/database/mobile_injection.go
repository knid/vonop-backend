package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateMobileInjection(injection *models.MobileInjection) (*models.MobileInjection, error) {
	_, err := db.DB.NewInsert().Model(injection).Returning("*").Exec(context.Background())
	return injection, err
}

func (db *Database) GetMobileInjectionsByProject(projectId string) ([]models.MobileInjection, error) {
	var injections []models.MobileInjection
	err := db.DB.NewSelect().Model(&injections).
		Where("project_id = ?", projectId).
		Order("sort_order ASC", "created_at ASC").
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return injections, nil
}

func (db *Database) GetMobileInjection(id string) (*models.MobileInjection, error) {
	var injection models.MobileInjection
	err := db.DB.NewSelect().Model(&injection).Where("id = ?", id).Scan(context.Background())
	return &injection, err
}

func (db *Database) UpdateMobileInjection(injection *models.MobileInjection) (*models.MobileInjection, error) {
	_, err := db.DB.NewUpdate().Model(injection).
		Where("id = ?", injection.ID).
		Exec(context.Background())
	return injection, err
}

func (db *Database) DeleteMobileInjection(id string) error {
	_, err := db.DB.NewDelete().
		Model((*models.MobileInjection)(nil)).
		Where("id = ?", id).
		Exec(context.Background())
	return err
}

func (db *Database) DeleteMobileInjectionByName(projectId, name string) error {
	_, err := db.DB.NewDelete().
		Model((*models.MobileInjection)(nil)).
		Where("project_id = ? AND name = ?", projectId, name).
		Exec(context.Background())
	return err
}

func (db *Database) GetEnabledMobileInjections(projectId string) ([]models.MobileInjection, error) {
	var injections []models.MobileInjection
	err := db.DB.NewSelect().Model(&injections).
		Where("project_id = ? AND is_enabled = true", projectId).
		Order("sort_order ASC", "created_at ASC").
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return injections, nil
}
