package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetCartBySessionID(storeID string, sessionID string) (*models.Cart, error) {
	var cart models.Cart
	err := db.DB.NewSelect().Model(&cart).Where("store_id = ? AND session_id = ?", storeID, sessionID).Scan(context.Background())
	return &cart, err
}

func (db *Database) GetCartByID(id string) (*models.Cart, error) {
	var cart models.Cart
	err := db.DB.NewSelect().Model(&cart).Where("id = ?", id).Scan(context.Background())
	return &cart, err
}

func (db *Database) CreateCart(c *models.Cart) (*models.Cart, error) {
	_, err := db.DB.NewInsert().Model(c).Exec(context.Background())
	return c, err
}

func (db *Database) UpdateCart(c *models.Cart) (*models.Cart, error) {
	_, err := db.DB.NewUpdate().Model(c).Where("id = ?", c.ID).Exec(context.Background())
	return c, err
}

// Cart Items

func (db *Database) GetCartItemsByCartID(cartID string) ([]models.CartItem, error) {
	var items []models.CartItem
	err := db.DB.NewSelect().Model(&items).Where("cart_id = ?", cartID).Relation("Product").Relation("Variant").Scan(context.Background())
	return items, err
}

func (db *Database) GetCartItemByID(id string) (*models.CartItem, error) {
	var item models.CartItem
	err := db.DB.NewSelect().Model(&item).Where("id = ?", id).Scan(context.Background())
	return &item, err
}

func (db *Database) CreateCartItem(ci *models.CartItem) (*models.CartItem, error) {
	_, err := db.DB.NewInsert().Model(ci).Exec(context.Background())
	return ci, err
}

func (db *Database) UpdateCartItem(ci *models.CartItem) (*models.CartItem, error) {
	_, err := db.DB.NewUpdate().Model(ci).Where("id = ?", ci.ID).Exec(context.Background())
	return ci, err
}

func (db *Database) DeleteCartItem(ci *models.CartItem) error {
	_, err := db.DB.NewDelete().Model(ci).Where("id = ?", ci.ID).Exec(context.Background())
	return err
}
