package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateNavBarItems(navBarItems *models.NavBarItems) (*models.NavBarItems, error) {
	_, err := db.DB.NewInsert().Model(navBarItems).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return navBarItems, nil
}

func (db *Database) GetNavBarItemsByProject(projectId string) (*models.NavBarItems, error) {
	var navBarItems models.NavBarItems

	err := db.DB.NewSelect().Model(&navBarItems).Where("project_id = ?", projectId).Scan(context.Background())
	if err != nil {
		return nil, err
	}

	return &navBarItems, nil
}

func (db *Database) UpdateNavBarItems(navBarItems *models.NavBarItems) (*models.NavBarItems, error) {
	_, err := db.DB.NewUpdate().Model(navBarItems).Where("id = ?", navBarItems.ID.String()).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	return navBarItems, nil
}

func (db *Database) DeleteNavBarItems(navBarItems *models.NavBarItems) error {
	_, err := db.DB.NewDelete().Model(navBarItems).Where("id = ?", navBarItems.ID.String()).Exec(context.Background())

	return err
}
