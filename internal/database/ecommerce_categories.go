package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetCategoriesByStoreID(storeID string) ([]models.ProductCategory, error) {
	var categories []models.ProductCategory
	err := db.DB.NewSelect().Model(&categories).Where("store_id = ?", storeID).Order("sort_order ASC").Scan(context.Background())
	return categories, err
}

func (db *Database) GetCategoryByID(id string) (*models.ProductCategory, error) {
	var category models.ProductCategory
	err := db.DB.NewSelect().Model(&category).Where("id = ?", id).Scan(context.Background())
	return &category, err
}

func (db *Database) CreateCategory(c *models.ProductCategory) (*models.ProductCategory, error) {
	_, err := db.DB.NewInsert().Model(c).Exec(context.Background())
	return c, err
}

func (db *Database) UpdateCategory(c *models.ProductCategory) (*models.ProductCategory, error) {
	_, err := db.DB.NewUpdate().Model(c).Where("id = ?", c.ID).Exec(context.Background())
	return c, err
}

func (db *Database) DeleteCategory(c *models.ProductCategory) error {
	_, err := db.DB.NewDelete().Model(c).Where("id = ?", c.ID).Exec(context.Background())
	return err
}
