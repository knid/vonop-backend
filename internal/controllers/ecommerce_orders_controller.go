package controllers

import (
	"fmt"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (c *Controller) GetOrders(storeID string) ([]models.Order, error) {
	return c.DB.GetOrdersByStoreID(storeID)
}

func (c *Controller) GetOrder(orderID string, store *models.Store) (*models.Order, error) {
	order, err := c.DB.GetOrderByID(orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found")
	}
	if order.StoreID != store.ID {
		return nil, fmt.Errorf("unauthorized")
	}
	return order, nil
}

func (c *Controller) GetOrderItems(orderID string) ([]models.OrderItem, error) {
	return c.DB.GetOrderItemsByOrderID(orderID)
}

func (c *Controller) UpdateOrderStatus(orderID string, store *models.Store, status string) (*models.Order, error) {
	order, err := c.GetOrder(orderID, store)
	if err != nil {
		return nil, err
	}

	order.Status = status
	order.UpdatedAt = time.Now()
	return c.DB.UpdateOrder(order)
}

func (c *Controller) GetStoreAnalytics(store *models.Store) (map[string]any, error) {
	orders, err := c.DB.GetOrdersByStoreID(store.ID.String())
	if err != nil {
		return nil, err
	}

	products, err := c.DB.GetProductsByStoreID(store.ID.String())
	if err != nil {
		return nil, err
	}

	totalRevenue := 0
	pendingOrders := 0
	for _, o := range orders {
		totalRevenue += o.TotalCents
		if o.Status == "pending" {
			pendingOrders++
		}
	}

	avgOrderValue := 0
	if len(orders) > 0 {
		avgOrderValue = totalRevenue / len(orders)
	}

	return map[string]any{
		"total_orders":              len(orders),
		"total_revenue_cents":       totalRevenue,
		"total_products":            len(products),
		"pending_orders":            pendingOrders,
		"average_order_value_cents": avgOrderValue,
	}, nil
}
