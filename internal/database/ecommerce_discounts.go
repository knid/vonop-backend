package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetDiscountsByStoreID(storeID string) ([]models.Discount, error) {
	var discounts []models.Discount
	err := db.DB.NewSelect().Model(&discounts).Where("store_id = ?", storeID).Order("created_at DESC").Scan(context.Background())
	return discounts, err
}

func (db *Database) GetDiscountByID(id string) (*models.Discount, error) {
	var discount models.Discount
	err := db.DB.NewSelect().Model(&discount).Where("id = ?", id).Scan(context.Background())
	return &discount, err
}

func (db *Database) GetDiscountByCode(storeID string, code string) (*models.Discount, error) {
	var discount models.Discount
	err := db.DB.NewSelect().Model(&discount).Where("store_id = ? AND code = ? AND is_active = true", storeID, code).Scan(context.Background())
	return &discount, err
}

func (db *Database) CreateDiscount(d *models.Discount) (*models.Discount, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) UpdateDiscount(d *models.Discount) (*models.Discount, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}

func (db *Database) DeleteDiscount(d *models.Discount) error {
	_, err := db.DB.NewDelete().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return err
}
