package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateJSInjectionData(jsInjectionData *models.JSInjectData) (*models.JSInjectData, error) {
	_, err := db.DB.NewInsert().Model(jsInjectionData).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return jsInjectionData, nil
}

func (db *Database) GetJSInjectionDataByProject(projectId string) (*models.JSInjectData, error) {
	var jsInjectionData models.JSInjectData

	err := db.DB.NewSelect().Model(&jsInjectionData).Where("project_id = ?", projectId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return &jsInjectionData, nil
}

func (db *Database) UpdateJSInjectionData(jsInjectionData *models.JSInjectData) (*models.JSInjectData, error) {
	_, err := db.DB.NewUpdate().Model(jsInjectionData).Where("id = ?", jsInjectionData.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return jsInjectionData, nil
}

func (db *Database) DeleteJSInjectionData(jsInjectionData *models.JSInjectData) error {
	_, err := db.DB.NewDelete().Model(jsInjectionData).Where("id = ?", jsInjectionData.ID.String()).Exec(context.Background())

	return err
}
