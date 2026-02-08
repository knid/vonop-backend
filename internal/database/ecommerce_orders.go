package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetOrdersByStoreID(storeID string) ([]models.Order, error) {
	var orders []models.Order
	err := db.DB.NewSelect().Model(&orders).Where("store_id = ?", storeID).Order("created_at DESC").Scan(context.Background())
	return orders, err
}

func (db *Database) GetOrderByID(id string) (*models.Order, error) {
	var order models.Order
	err := db.DB.NewSelect().Model(&order).Where("id = ?", id).Scan(context.Background())
	return &order, err
}

func (db *Database) GetOrderByOrderNumber(orderNumber string) (*models.Order, error) {
	var order models.Order
	err := db.DB.NewSelect().Model(&order).Where("order_number = ?", orderNumber).Scan(context.Background())
	return &order, err
}

func (db *Database) CreateOrder(o *models.Order) (*models.Order, error) {
	_, err := db.DB.NewInsert().Model(o).Exec(context.Background())
	return o, err
}

func (db *Database) UpdateOrder(o *models.Order) (*models.Order, error) {
	_, err := db.DB.NewUpdate().Model(o).Where("id = ?", o.ID).Exec(context.Background())
	return o, err
}

// Order Items

func (db *Database) GetOrderItemsByOrderID(orderID string) ([]models.OrderItem, error) {
	var items []models.OrderItem
	err := db.DB.NewSelect().Model(&items).Where("order_id = ?", orderID).Scan(context.Background())
	return items, err
}

func (db *Database) CreateOrderItem(oi *models.OrderItem) (*models.OrderItem, error) {
	_, err := db.DB.NewInsert().Model(oi).Exec(context.Background())
	return oi, err
}

func (db *Database) BulkCreateOrderItems(items []models.OrderItem) error {
	if len(items) == 0 {
		return nil
	}
	_, err := db.DB.NewInsert().Model(&items).Exec(context.Background())
	return err
}

func (db *Database) CountOrdersByStoreID(storeID string) (int, error) {
	count, err := db.DB.NewSelect().Model((*models.Order)(nil)).Where("store_id = ?", storeID).Count(context.Background())
	return count, err
}
