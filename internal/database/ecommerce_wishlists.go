package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetWishlistBySessionID(storeID string, sessionID string) ([]models.Wishlist, error) {
	var items []models.Wishlist
	err := db.DB.NewSelect().
		Model(&items).
		Where("store_id = ? AND session_id = ?", storeID, sessionID).
		Order("created_at DESC").
		Scan(context.Background())
	return items, err
}

func (db *Database) AddToWishlist(item *models.Wishlist) (*models.Wishlist, error) {
	_, err := db.DB.NewInsert().Model(item).Exec(context.Background())
	return item, err
}

func (db *Database) RemoveFromWishlist(storeID string, sessionID string, productID string) error {
	_, err := db.DB.NewDelete().
		Model((*models.Wishlist)(nil)).
		Where("store_id = ? AND session_id = ? AND product_id = ?", storeID, sessionID, productID).
		Exec(context.Background())
	return err
}

func (db *Database) IsInWishlist(storeID string, sessionID string, productID string) (bool, error) {
	exists, err := db.DB.NewSelect().
		Model((*models.Wishlist)(nil)).
		Where("store_id = ? AND session_id = ? AND product_id = ?", storeID, sessionID, productID).
		Exists(context.Background())
	return exists, err
}
