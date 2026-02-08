package controllers

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// Storefront - public facing API for generated storefronts

func (c *Controller) StorefrontGetProducts(storeID string) ([]models.Product, error) {
	products, err := c.DB.GetProductsByStoreID(storeID)
	if err != nil {
		return nil, err
	}
	// Only return active products
	var active []models.Product
	for _, p := range products {
		if p.Status == "active" {
			active = append(active, p)
		}
	}
	return active, nil
}

func (c *Controller) StorefrontGetProduct(storeID string, slug string) (*models.Product, []models.ProductVariant, error) {
	product, err := c.DB.GetProductBySlug(storeID, slug)
	if err != nil {
		return nil, nil, fmt.Errorf("product not found")
	}
	if product.Status != "active" {
		return nil, nil, fmt.Errorf("product not found")
	}
	variants, _ := c.DB.GetVariantsByProductID(product.ID.String())
	return product, variants, nil
}

func (c *Controller) StorefrontGetCategories(storeID string) ([]models.ProductCategory, error) {
	return c.DB.GetCategoriesByStoreID(storeID)
}

func (c *Controller) StorefrontCreateCart(storeID string, sessionID string) (*models.Cart, error) {
	// Check if cart already exists for this session
	existing, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err == nil {
		return existing, nil
	}

	sid, _ := uuid.Parse(storeID)
	cart := &models.Cart{
		StoreID:   sid,
		SessionID: sessionID,
	}
	return c.DB.CreateCart(cart)
}

func (c *Controller) StorefrontGetCart(storeID string, sessionID string) (*models.Cart, []models.CartItem, error) {
	cart, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("cart not found")
	}
	items, _ := c.DB.GetCartItemsByCartID(cart.ID.String())
	return cart, items, nil
}

func (c *Controller) StorefrontAddCartItem(storeID string, sessionID string, productID string, variantID string, quantity int) (*models.CartItem, error) {
	cart, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("cart not found")
	}

	product, err := c.DB.GetProductByID(productID)
	if err != nil {
		return nil, fmt.Errorf("product not found")
	}

	// Determine price from variant or default variant
	price := 0
	var vid uuid.UUID
	if variantID != "" {
		vid, _ = uuid.Parse(variantID)
		variant, err := c.DB.GetVariantByID(variantID)
		if err != nil {
			return nil, fmt.Errorf("variant not found")
		}
		price = variant.PriceInCents
	} else {
		// Use default variant
		variants, _ := c.DB.GetVariantsByProductID(productID)
		for _, v := range variants {
			if v.IsDefault {
				vid = v.ID
				price = v.PriceInCents
				break
			}
		}
		if price == 0 && len(variants) > 0 {
			vid = variants[0].ID
			price = variants[0].PriceInCents
		}
	}

	pid, _ := uuid.Parse(productID)
	item := &models.CartItem{
		CartID:          cart.ID,
		ProductID:       pid,
		VariantID:       vid,
		Quantity:        quantity,
		PriceAtAddCents: price,
	}

	_ = product // used for validation above
	return c.DB.CreateCartItem(item)
}

func (c *Controller) StorefrontUpdateCartItem(itemID string, quantity int) (*models.CartItem, error) {
	item, err := c.DB.GetCartItemByID(itemID)
	if err != nil {
		return nil, fmt.Errorf("cart item not found")
	}

	if quantity <= 0 {
		return nil, c.DB.DeleteCartItem(item)
	}

	item.Quantity = quantity
	item.UpdatedAt = time.Now()
	return c.DB.UpdateCartItem(item)
}

func (c *Controller) StorefrontDeleteCartItem(itemID string) error {
	item, err := c.DB.GetCartItemByID(itemID)
	if err != nil {
		return fmt.Errorf("cart item not found")
	}
	return c.DB.DeleteCartItem(item)
}

func (c *Controller) StorefrontApplyDiscount(storeID string, sessionID string, code string) (*models.Discount, error) {
	cart, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("cart not found")
	}

	discount, err := c.DB.GetDiscountByCode(storeID, code)
	if err != nil {
		return nil, fmt.Errorf("invalid discount code")
	}

	if !discount.IsActive {
		return nil, fmt.Errorf("discount code is inactive")
	}
	if discount.MaxUses > 0 && discount.UsedCount >= discount.MaxUses {
		return nil, fmt.Errorf("discount code has reached maximum uses")
	}

	cart.DiscountID = discount.ID
	cart.UpdatedAt = time.Now()
	c.DB.UpdateCart(cart)

	return discount, nil
}

func (c *Controller) StorefrontCheckout(storeID string, sessionID string, customerEmail string, customerName string, shippingAddress map[string]any, billingAddress map[string]any, paymentProvider string) (*models.Order, error) {
	cart, items, err := c.StorefrontGetCart(storeID, sessionID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cart is empty")
	}

	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return nil, fmt.Errorf("store not found")
	}

	// Calculate totals
	subtotal := 0
	for _, item := range items {
		subtotal += item.PriceAtAddCents * item.Quantity
	}

	discountCents := 0
	if cart.DiscountID != uuid.Nil {
		discount, err := c.DB.GetDiscountByID(cart.DiscountID.String())
		if err == nil && discount.IsActive {
			if discount.Type == "percentage" {
				discountCents = subtotal * discount.Value / 100
			} else {
				discountCents = discount.Value
			}
			if discountCents > subtotal {
				discountCents = subtotal
			}
		}
	}

	taxCents := int(float64(subtotal-discountCents) * store.TaxRate / 100)
	totalCents := subtotal - discountCents + taxCents

	// Generate order number
	orderCount, _ := c.DB.CountOrdersByStoreID(storeID)
	orderNumber := fmt.Sprintf("ORD-%06d", orderCount+1)

	if billingAddress == nil {
		billingAddress = shippingAddress
	}

	order := &models.Order{
		StoreID:         store.ID,
		OrderNumber:     orderNumber,
		CustomerEmail:   customerEmail,
		CustomerName:    customerName,
		ShippingAddress: shippingAddress,
		BillingAddress:  billingAddress,
		SubtotalCents:   subtotal,
		DiscountCents:   discountCents,
		TaxCents:        taxCents,
		TotalCents:      totalCents,
		Currency:        store.Currency,
		Status:          "pending",
		PaymentStatus:   "pending",
		PaymentProvider: paymentProvider,
		DiscountID:      cart.DiscountID,
		Metadata:        map[string]any{},
	}

	order, err = c.DB.CreateOrder(order)
	if err != nil {
		return nil, fmt.Errorf("failed to create order")
	}

	// Create order items
	var orderItems []models.OrderItem
	for _, item := range items {
		product, _ := c.DB.GetProductByID(item.ProductID.String())
		productName := ""
		if product != nil {
			productName = product.Name
		}
		variantName := ""
		if item.VariantID != uuid.Nil {
			variant, _ := c.DB.GetVariantByID(item.VariantID.String())
			if variant != nil {
				variantName = variant.Name
			}
		}

		orderItems = append(orderItems, models.OrderItem{
			OrderID:      order.ID,
			ProductID:    item.ProductID,
			VariantID:    item.VariantID,
			ProductName:  productName,
			VariantName:  variantName,
			Quantity:     item.Quantity,
			PriceInCents: item.PriceAtAddCents,
			TotalCents:   item.PriceAtAddCents * item.Quantity,
		})
	}
	c.DB.BulkCreateOrderItems(orderItems)

	return order, nil
}

func (c *Controller) StorefrontConfirmPayment(orderID string, paymentID string) (*models.Order, error) {
	order, err := c.DB.GetOrderByID(orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found")
	}

	order.PaymentID = paymentID
	order.PaymentStatus = "paid"
	order.Status = "confirmed"
	order.UpdatedAt = time.Now()

	return c.DB.UpdateOrder(order)
}
