package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetProductsByStoreID(storeID string) ([]models.Product, error) {
	var products []models.Product
	err := db.DB.NewSelect().Model(&products).Where("store_id = ?", storeID).Order("created_at DESC").Scan(context.Background())
	return products, err
}

func (db *Database) GetProductByID(id string) (*models.Product, error) {
	var product models.Product
	err := db.DB.NewSelect().Model(&product).Where("id = ?", id).Scan(context.Background())
	return &product, err
}

func (db *Database) GetProductBySlug(storeID string, slug string) (*models.Product, error) {
	var product models.Product
	err := db.DB.NewSelect().Model(&product).Where("store_id = ? AND slug = ?", storeID, slug).Scan(context.Background())
	return &product, err
}

func (db *Database) CreateProduct(p *models.Product) (*models.Product, error) {
	_, err := db.DB.NewInsert().Model(p).Exec(context.Background())
	return p, err
}

func (db *Database) UpdateProduct(p *models.Product) (*models.Product, error) {
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return p, err
}

func (db *Database) DeleteProduct(p *models.Product) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return err
}

// Product Variants

func (db *Database) GetVariantsByProductID(productID string) ([]models.ProductVariant, error) {
	var variants []models.ProductVariant
	err := db.DB.NewSelect().Model(&variants).Where("product_id = ?", productID).Scan(context.Background())
	return variants, err
}

func (db *Database) GetVariantByID(id string) (*models.ProductVariant, error) {
	var variant models.ProductVariant
	err := db.DB.NewSelect().Model(&variant).Where("id = ?", id).Scan(context.Background())
	return &variant, err
}

func (db *Database) CreateVariant(v *models.ProductVariant) (*models.ProductVariant, error) {
	_, err := db.DB.NewInsert().Model(v).Exec(context.Background())
	return v, err
}

func (db *Database) UpdateVariant(v *models.ProductVariant) (*models.ProductVariant, error) {
	_, err := db.DB.NewUpdate().Model(v).Where("id = ?", v.ID).Exec(context.Background())
	return v, err
}

func (db *Database) DeleteVariant(v *models.ProductVariant) error {
	_, err := db.DB.NewDelete().Model(v).Where("id = ?", v.ID).Exec(context.Background())
	return err
}

func (db *Database) GetVariantsByStoreID(storeID string) ([]models.ProductVariant, error) {
	var variants []models.ProductVariant
	err := db.DB.NewSelect().
		Model(&variants).
		Join("JOIN products ON products.id = product_variant.product_id").
		Where("products.store_id = ?", storeID).
		Where("products.deleted_at IS NULL").
		Scan(context.Background())
	return variants, err
}
