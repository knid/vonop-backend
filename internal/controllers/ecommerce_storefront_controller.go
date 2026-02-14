package controllers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

// Storefront - public facing API for generated storefronts

// StorefrontGuard validates that a store exists and is active.
func (c *Controller) StorefrontGuard(storeID string) (*models.Store, error) {
	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return nil, fmt.Errorf("store not found")
	}
	if store.Status != "active" {
		return nil, fmt.Errorf("store is not active")
	}
	return store, nil
}

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
		// Fix 8: Check inventory before adding to cart
		if variant.TrackInventory && variant.Inventory < quantity {
			return nil, fmt.Errorf("insufficient stock: only %d available", variant.Inventory)
		}
	} else {
		// Use default variant
		variants, _ := c.DB.GetVariantsByProductID(productID)
		for _, v := range variants {
			if v.IsDefault {
				vid = v.ID
				price = v.PriceInCents
				if v.TrackInventory && v.Inventory < quantity {
					return nil, fmt.Errorf("insufficient stock: only %d available", v.Inventory)
				}
				break
			}
		}
		if price == 0 && len(variants) > 0 {
			vid = variants[0].ID
			price = variants[0].PriceInCents
			if variants[0].TrackInventory && variants[0].Inventory < quantity {
				return nil, fmt.Errorf("insufficient stock: only %d available", variants[0].Inventory)
			}
		}
	}

	// Fix 22: Reject items with no valid price or variant
	if price == 0 {
		return nil, fmt.Errorf("product has no valid price configuration")
	}
	if vid == uuid.Nil {
		return nil, fmt.Errorf("no variant available for this product")
	}

	// Fix 10: Merge duplicate cart items
	existingItems, _ := c.DB.GetCartItemsByCartID(cart.ID.String())
	for _, existing := range existingItems {
		if existing.ProductID.String() == productID && existing.VariantID == vid {
			existing.Quantity += quantity
			existing.UpdatedAt = time.Now()
			return c.DB.UpdateCartItem(&existing)
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

// Fix 2: StorefrontUpdateCartItem verifies item belongs to the session's cart.
func (c *Controller) StorefrontUpdateCartItem(storeID string, sessionID string, itemID string, quantity int) (*models.CartItem, error) {
	cart, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("cart not found")
	}

	item, err := c.DB.GetCartItemByID(itemID)
	if err != nil {
		return nil, fmt.Errorf("cart item not found")
	}

	if item.CartID != cart.ID {
		return nil, fmt.Errorf("cart item not found")
	}

	if quantity <= 0 {
		return nil, c.DB.DeleteCartItem(item)
	}

	item.Quantity = quantity
	item.UpdatedAt = time.Now()
	return c.DB.UpdateCartItem(item)
}

// Fix 2: StorefrontDeleteCartItem verifies item belongs to the session's cart.
func (c *Controller) StorefrontDeleteCartItem(storeID string, sessionID string, itemID string) error {
	cart, err := c.DB.GetCartBySessionID(storeID, sessionID)
	if err != nil {
		return fmt.Errorf("cart not found")
	}

	item, err := c.DB.GetCartItemByID(itemID)
	if err != nil {
		return fmt.Errorf("cart item not found")
	}

	if item.CartID != cart.ID {
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

	// Fix 11: Time-based validation
	now := time.Now()
	if !discount.StartsAt.IsZero() && now.Before(discount.StartsAt) {
		return nil, fmt.Errorf("discount code is not yet active")
	}
	if !discount.EndsAt.IsZero() && now.After(discount.EndsAt) {
		return nil, fmt.Errorf("discount code has expired")
	}

	cart.DiscountID = discount.ID
	cart.UpdatedAt = time.Now()
	c.DB.UpdateCart(cart)

	return discount, nil
}

func (c *Controller) StorefrontCheckout(storeID string, sessionID string, customerEmail string, customerName string, shippingAddress map[string]any, billingAddress map[string]any, paymentProvider string) (*models.Order, string, string, error) {
	cart, items, err := c.StorefrontGetCart(storeID, sessionID)
	if err != nil {
		return nil, "", "", err
	}
	if len(items) == 0 {
		return nil, "", "", fmt.Errorf("cart is empty")
	}

	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return nil, "", "", fmt.Errorf("store not found")
	}

	// Fix 8: Re-validate inventory at checkout time
	for _, item := range items {
		if item.VariantID != uuid.Nil {
			variant, err := c.DB.GetVariantByID(item.VariantID.String())
			if err == nil && variant.TrackInventory && variant.Inventory < item.Quantity {
				productName := ""
				if p, _ := c.DB.GetProductByID(item.ProductID.String()); p != nil {
					productName = p.Name
				}
				return nil, "", "", fmt.Errorf("insufficient stock for %s: only %d available", productName, variant.Inventory)
			}
		}
	}

	// Calculate totals
	subtotal := 0
	for _, item := range items {
		subtotal += item.PriceAtAddCents * item.Quantity
	}

	discountCents := 0
	var discount *models.Discount
	if cart.DiscountID != uuid.Nil {
		d, err := c.DB.GetDiscountByID(cart.DiscountID.String())
		if err == nil && d.IsActive {
			discount = d
			// Fix 12: Enforce MinOrderCents
			if discount.MinOrderCents > 0 && subtotal < discount.MinOrderCents {
				discountCents = 0
			} else {
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
	}

	taxCents := int(float64(subtotal-discountCents) * store.TaxRate / 100)

	// Fix 21: Basic shipping cost calculation
	shippingCents := 0
	if len(store.ShippingConfig) > 0 {
		if flatRate, ok := store.ShippingConfig["flat_rate"].(float64); ok {
			shippingCents = int(flatRate)
		}
		if freeAbove, ok := store.ShippingConfig["free_above"].(float64); ok {
			if subtotal >= int(freeAbove) {
				shippingCents = 0
			}
		}
	}

	totalCents := subtotal - discountCents + taxCents + shippingCents

	if billingAddress == nil {
		billingAddress = shippingAddress
	}

	// Fix 9: Atomic order number generation with retry
	var order *models.Order
	for attempts := 0; attempts < 5; attempts++ {
		orderCount, _ := c.DB.CountOrdersByStoreID(storeID)
		orderNumber := fmt.Sprintf("ORD-%06d", orderCount+1)

		order = &models.Order{
			StoreID:         store.ID,
			OrderNumber:     orderNumber,
			CustomerEmail:   customerEmail,
			CustomerName:    customerName,
			ShippingAddress: shippingAddress,
			BillingAddress:  billingAddress,
			SubtotalCents:   subtotal,
			DiscountCents:   discountCents,
			TaxCents:        taxCents,
			ShippingCents:   shippingCents,
			TotalCents:      totalCents,
			Currency:        store.Currency,
			Status:          "pending",
			PaymentStatus:   "pending",
			PaymentProvider: paymentProvider,
			DiscountID:      cart.DiscountID,
			Metadata:        map[string]any{},
		}

		order, err = c.DB.CreateOrder(order)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "duplicate") {
			continue
		}
		return nil, "", "", fmt.Errorf("failed to create order")
	}
	if order == nil {
		return nil, "", "", fmt.Errorf("failed to create order after retries")
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

	// Fix 5: Increment discount UsedCount
	if discount != nil {
		discount.UsedCount++
		discount.UpdatedAt = time.Now()
		c.DB.UpdateDiscount(discount)
	}

	// Fix 7: Decrement inventory
	for _, item := range items {
		if item.VariantID != uuid.Nil {
			variant, err := c.DB.GetVariantByID(item.VariantID.String())
			if err == nil && variant.TrackInventory {
				variant.Inventory -= item.Quantity
				if variant.Inventory < 0 {
					variant.Inventory = 0
				}
				variant.UpdatedAt = time.Now()
				c.DB.UpdateVariant(variant)
			}
		}
	}

	// Fix 6: Clear cart after checkout
	for i := range items {
		c.DB.DeleteCartItem(&items[i])
	}
	cart.DiscountID = uuid.Nil
	cart.UpdatedAt = time.Now()
	c.DB.UpdateCart(cart)

	// Initiate payment if a payment provider is specified and connected
	paymentURL, clientSecret, err := c.StorefrontInitiatePayment(storeID, order, orderItems)
	if err != nil {
		slog.Warn("Payment initiation failed, order created without payment", "error", err, "order_id", order.ID)
		// Order still exists with pending payment — frontend can retry or use manual payment
	}

	// Fix 20: Send order notification email
	if store.NotifyEmail != "" {
		go func() {
			_, emailErr := c.EmailService.SendEmail(&models.User{Email: store.NotifyEmail},
				constants.BREVO_ORDER_NOTIFICATION_TEMPLATE_ID, map[string]any{
					"ORDER_NUMBER":  order.OrderNumber,
					"CUSTOMER_NAME": order.CustomerName,
					"TOTAL":         fmt.Sprintf("%.2f %s", float64(order.TotalCents)/100, order.Currency),
					"ITEMS_COUNT":   len(orderItems),
				})
			if emailErr != nil {
				slog.Warn("Failed to send order notification email", "error", emailErr, "store_id", storeID)
			}
		}()
	}

	return order, paymentURL, clientSecret, nil
}

// Fix 1: StorefrontConfirmPayment verifies payment server-side before marking as paid.
func (c *Controller) StorefrontConfirmPayment(storeID string, orderID string, paymentID string) (*models.Order, error) {
	order, err := c.DB.GetOrderByID(orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found")
	}
	if order.StoreID.String() != storeID {
		return nil, fmt.Errorf("order not found")
	}

	// If order is already confirmed, return it
	if order.PaymentStatus == "paid" {
		return order, nil
	}

	// Verify payment server-side based on provider
	if order.PaymentProvider != "" {
		store, err := c.DB.GetStoreByID(storeID)
		if err != nil {
			return nil, fmt.Errorf("store not found")
		}

		integrations, err := c.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
		if err != nil {
			return nil, fmt.Errorf("payment verification failed: no integrations")
		}

		var paymentInteg *models.AppIntegration
		for _, integ := range integrations {
			if integ.Provider == order.PaymentProvider && integ.Status == "connected" {
				paymentInteg = &integ
				break
			}
		}
		if paymentInteg == nil {
			return nil, fmt.Errorf("payment verification failed: provider not connected")
		}

		var creds map[string]any
		if err := json.Unmarshal(paymentInteg.Credentials, &creds); err != nil {
			return nil, fmt.Errorf("payment verification failed: invalid credentials")
		}

		switch order.PaymentProvider {
		case "stripe":
			if err := c.verifyStripePayment(paymentID, order, creds); err != nil {
				return nil, fmt.Errorf("payment verification failed: %s", err.Error())
			}
		case "iyzico":
			if err := c.verifyIyzicoPayment(paymentID, creds); err != nil {
				return nil, fmt.Errorf("payment verification failed: %s", err.Error())
			}
		default:
			return nil, fmt.Errorf("unsupported payment provider: %s", order.PaymentProvider)
		}
	}

	order.PaymentID = paymentID
	order.PaymentStatus = "paid"
	order.Status = "confirmed"
	order.UpdatedAt = time.Now()

	return c.DB.UpdateOrder(order)
}

// verifyStripePayment checks a Stripe checkout session is actually paid.
func (c *Controller) verifyStripePayment(paymentID string, order *models.Order, creds map[string]any) error {
	secretKey, _ := creds["secret_key"].(string)
	if secretKey == "" {
		return fmt.Errorf("stripe secret key not configured")
	}

	sc := &client.API{}
	sc.Init(secretKey, nil)

	session, err := sc.CheckoutSessions.Get(paymentID, nil)
	if err != nil {
		return fmt.Errorf("failed to retrieve checkout session: %w", err)
	}

	if session.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return fmt.Errorf("payment not completed (status: %s)", session.PaymentStatus)
	}

	// Verify the session belongs to this order
	if session.Metadata != nil {
		if sessionOrderID, ok := session.Metadata["order_id"]; ok {
			if sessionOrderID != order.ID.String() {
				return fmt.Errorf("payment session does not match order")
			}
		}
	}

	return nil
}

// verifyIyzicoPayment checks an Iyzico payment is actually successful.
func (c *Controller) verifyIyzicoPayment(token string, creds map[string]any) error {
	apiKey, _ := creds["api_key"].(string)
	secretKey, _ := creds["secret_key"].(string)
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("iyzico credentials not configured")
	}

	baseURL := iyzicoBaseURL(creds)
	uri := "/payment/iyzipos/checkoutform/auth/ecom/detail"
	body, _ := json.Marshal(map[string]any{
		"locale": "en",
		"token":  token,
	})

	respBody, err := iyzicoRequest(apiKey, secretKey, baseURL, uri, body)
	if err != nil {
		return fmt.Errorf("failed to verify with iyzico: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("invalid iyzico response")
	}

	status, _ := result["status"].(string)
	if status != "success" {
		errMsg, _ := result["errorMessage"].(string)
		return fmt.Errorf("iyzico payment not successful: %s", errMsg)
	}

	paymentStatus, _ := result["paymentStatus"].(string)
	if paymentStatus != "SUCCESS" {
		return fmt.Errorf("iyzico payment status: %s", paymentStatus)
	}

	return nil
}

func (c *Controller) StorefrontGetStoreConfig(storeID string) (*models.Store, []string, error) {
	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return nil, nil, fmt.Errorf("store not found")
	}

	// Discover connected payment providers via AppIntegration
	var paymentProviders []string
	integrations, err := c.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	if err == nil {
		for _, integ := range integrations {
			if integ.Status != "connected" {
				continue
			}
			if integ.Provider == "stripe" || integ.Provider == "iyzico" {
				paymentProviders = append(paymentProviders, integ.Provider)
			}
		}
	}

	return store, paymentProviders, nil
}

func (c *Controller) StorefrontGetOrder(storeID string, orderID string) (*models.Order, []models.OrderItem, error) {
	order, err := c.DB.GetOrderByID(orderID)
	if err != nil {
		return nil, nil, fmt.Errorf("order not found")
	}
	if order.StoreID.String() != storeID {
		return nil, nil, fmt.Errorf("order not found")
	}

	items, _ := c.DB.GetOrderItemsByOrderID(orderID)
	return order, items, nil
}

// StorefrontInitiatePayment creates a payment session for a given order.
// Returns (paymentURL, clientSecret, error). Only one of paymentURL/clientSecret will be non-empty.
func (c *Controller) StorefrontInitiatePayment(storeID string, order *models.Order, orderItems []models.OrderItem) (string, string, error) {
	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return "", "", fmt.Errorf("store not found")
	}

	integrations, err := c.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	if err != nil {
		return "", "", fmt.Errorf("no integrations found")
	}

	// Find the matching payment integration
	var paymentInteg *models.AppIntegration
	for _, integ := range integrations {
		if integ.Provider == order.PaymentProvider && integ.Status == "connected" {
			paymentInteg = &integ
			break
		}
	}
	if paymentInteg == nil {
		return "", "", fmt.Errorf("payment provider %s not connected", order.PaymentProvider)
	}

	var creds map[string]any
	if err := json.Unmarshal(paymentInteg.Credentials, &creds); err != nil {
		return "", "", fmt.Errorf("invalid payment credentials")
	}

	switch order.PaymentProvider {
	case "stripe":
		return c.initiateStripePayment(store, order, orderItems, creds)
	case "iyzico":
		return c.initiateIyzicoPayment(store, order, orderItems, creds)
	default:
		return "", "", fmt.Errorf("unsupported payment provider: %s", order.PaymentProvider)
	}
}

func (c *Controller) initiateStripePayment(store *models.Store, order *models.Order, orderItems []models.OrderItem, creds map[string]any) (string, string, error) {
	secretKey, _ := creds["secret_key"].(string)
	if secretKey == "" {
		return "", "", fmt.Errorf("stripe secret key not configured")
	}

	// Create a per-store Stripe client (uses the store's own secret key, not the platform key)
	sc := &client.API{}
	sc.Init(secretKey, nil)

	// Build Stripe line items from order items
	var lineItems []*stripe.CheckoutSessionLineItemParams
	for _, item := range orderItems {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripe.String(strings.ToLower(order.Currency)),
				UnitAmount: stripe.Int64(int64(item.PriceInCents)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(item.ProductName),
				},
			},
			Quantity: stripe.Int64(int64(item.Quantity)),
		})
	}

	// Determine success/cancel URLs from the project slug
	project, _ := c.DB.GetAppProjectByID(store.AppProjectID.String())
	baseURL := fmt.Sprintf("https://%s.%s", store.AppProjectID.String()[:8], constants.WEMOB_APP_BASE_DOMAIN)
	if project != nil {
		baseURL = fmt.Sprintf("https://%s.%s", project.Slug, constants.WEMOB_APP_BASE_DOMAIN)
	}

	successURL := baseURL + "/checkout/success?order_id=" + order.ID.String()
	cancelURL := baseURL + "/checkout/cancel?order_id=" + order.ID.String()

	params := &stripe.CheckoutSessionParams{
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems:     lineItems,
		SuccessURL:    stripe.String(successURL),
		CancelURL:     stripe.String(cancelURL),
		CustomerEmail: stripe.String(order.CustomerEmail),
	}
	params.AddMetadata("order_id", order.ID.String())
	params.AddMetadata("order_number", order.OrderNumber)
	params.AddMetadata("store_id", store.ID.String())

	session, err := sc.CheckoutSessions.New(params)
	if err != nil {
		slog.Error("Stripe checkout session creation failed", "error", err, "store_id", store.ID)
		return "", "", fmt.Errorf("failed to create stripe checkout session")
	}

	return session.URL, "", nil
}

// Fix 13: Full Iyzico server-side implementation
func (c *Controller) initiateIyzicoPayment(store *models.Store, order *models.Order, orderItems []models.OrderItem, creds map[string]any) (string, string, error) {
	apiKey, _ := creds["api_key"].(string)
	secretKey, _ := creds["secret_key"].(string)
	if apiKey == "" || secretKey == "" {
		return "", "", fmt.Errorf("iyzico credentials not configured")
	}

	baseURL := iyzicoBaseURL(creds)

	// Build callback URL pointing to our server endpoint
	callbackURL := fmt.Sprintf("%s/v2/storefront/%s/checkout/iyzico-callback?order_id=%s",
		constants.WEMOB_API_BASE_URL, store.ID.String(), order.ID.String())

	// Build basket items for Iyzico
	var basketItems []map[string]any
	for _, item := range orderItems {
		basketItems = append(basketItems, map[string]any{
			"id":        item.ProductID.String(),
			"name":      item.ProductName,
			"category1": "General",
			"itemType":  "PHYSICAL",
			"price":     fmt.Sprintf("%.2f", float64(item.TotalCents)/100),
		})
	}

	iyzicoPayload := map[string]any{
		"locale":              store.Locale,
		"conversationId":      order.ID.String(),
		"price":               fmt.Sprintf("%.2f", float64(order.SubtotalCents)/100),
		"paidPrice":           fmt.Sprintf("%.2f", float64(order.TotalCents)/100),
		"currency":            order.Currency,
		"basketId":            order.OrderNumber,
		"paymentGroup":        "PRODUCT",
		"callbackUrl":         callbackURL,
		"enabledInstallments": []int{1, 2, 3, 6, 9},
		"buyer": map[string]any{
			"id":                  order.CustomerEmail,
			"name":                order.CustomerName,
			"email":               order.CustomerEmail,
			"registrationAddress": "N/A",
			"city":                "N/A",
			"country":             "N/A",
		},
		"basketItems": basketItems,
	}

	payloadJSON, err := json.Marshal(iyzicoPayload)
	if err != nil {
		return "", "", fmt.Errorf("failed to build iyzico request")
	}

	// Call Iyzico API
	uri := "/payment/iyzipos/checkoutform/initialize/auth/ecom"
	respBody, err := iyzicoRequest(apiKey, secretKey, baseURL, uri, payloadJSON)
	if err != nil {
		slog.Error("Iyzico API request failed", "error", err, "store_id", store.ID)
		return "", "", fmt.Errorf("failed to connect to iyzico")
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		slog.Error("Failed to parse Iyzico response", "error", err, "response", string(respBody))
		return "", "", fmt.Errorf("invalid response from iyzico")
	}

	status, _ := result["status"].(string)
	if status != "success" {
		errMsg, _ := result["errorMessage"].(string)
		slog.Error("Iyzico checkout form init failed", "error", errMsg, "store_id", store.ID)
		return "", "", fmt.Errorf("iyzico error: %s", errMsg)
	}

	// Extract checkout form content and token
	checkoutFormContent, _ := result["checkoutFormContent"].(string)
	token, _ := result["token"].(string)

	// Store token in order metadata for callback verification
	order.Metadata["iyzico_token"] = token
	order.Metadata["iyzico_callback_url"] = callbackURL
	order.UpdatedAt = time.Now()
	c.DB.UpdateOrder(order)

	slog.Info("Iyzico checkout form initialized", "store_id", store.ID, "order_id", order.ID, "has_form", checkoutFormContent != "")

	// Return the checkout form HTML as clientSecret for frontend rendering
	return "", checkoutFormContent, nil
}

// Fix 14: StorefrontHandleIyzicoCallback processes the Iyzico payment callback.
func (c *Controller) StorefrontHandleIyzicoCallback(storeID string, orderID string, token string) (string, error) {
	order, err := c.DB.GetOrderByID(orderID)
	if err != nil {
		return "", fmt.Errorf("order not found")
	}
	if order.StoreID.String() != storeID {
		return "", fmt.Errorf("order not found")
	}

	store, err := c.DB.GetStoreByID(storeID)
	if err != nil {
		return "", fmt.Errorf("store not found")
	}

	// Get payment credentials
	integrations, err := c.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	if err != nil {
		return "", fmt.Errorf("no integrations found")
	}

	var paymentInteg *models.AppIntegration
	for _, integ := range integrations {
		if integ.Provider == "iyzico" && integ.Status == "connected" {
			paymentInteg = &integ
			break
		}
	}
	if paymentInteg == nil {
		return "", fmt.Errorf("iyzico not connected")
	}

	var creds map[string]any
	if err := json.Unmarshal(paymentInteg.Credentials, &creds); err != nil {
		return "", fmt.Errorf("invalid payment credentials")
	}

	// Verify payment with Iyzico
	err = c.verifyIyzicoPayment(token, creds)

	// Determine redirect URL
	project, _ := c.DB.GetAppProjectByID(store.AppProjectID.String())
	baseURL := fmt.Sprintf("https://%s.%s", store.AppProjectID.String()[:8], constants.WEMOB_APP_BASE_DOMAIN)
	if project != nil {
		baseURL = fmt.Sprintf("https://%s.%s", project.Slug, constants.WEMOB_APP_BASE_DOMAIN)
	}

	if err != nil {
		// Payment failed
		order.PaymentStatus = "failed"
		order.Status = "cancelled"
		order.UpdatedAt = time.Now()
		c.DB.UpdateOrder(order)

		slog.Warn("Iyzico payment verification failed", "error", err, "order_id", orderID)
		return baseURL + "/checkout/failure?order_id=" + orderID, nil
	}

	// Payment succeeded
	order.PaymentID = token
	order.PaymentStatus = "paid"
	order.Status = "confirmed"
	order.UpdatedAt = time.Now()
	c.DB.UpdateOrder(order)

	slog.Info("Iyzico payment confirmed", "order_id", orderID, "store_id", storeID)
	return baseURL + "/checkout/success?order_id=" + orderID, nil
}

// Fix 15: StorefrontHandleStripeWebhook processes Stripe webhook events for per-store accounts.
func (c *Controller) StorefrontHandleStripeWebhook(body []byte) error {
	var event stripe.Event
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("invalid stripe event: %w", err)
	}

	switch event.Type {
	case "checkout.session.completed":
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			return fmt.Errorf("failed to parse checkout session: %w", err)
		}

		orderID, ok := session.Metadata["order_id"]
		if !ok || orderID == "" {
			slog.Warn("Stripe webhook: no order_id in metadata", "session_id", session.ID)
			return nil
		}

		order, err := c.DB.GetOrderByID(orderID)
		if err != nil {
			slog.Warn("Stripe webhook: order not found", "order_id", orderID)
			return nil
		}

		// Verify store_id matches
		if storeID, ok := session.Metadata["store_id"]; ok {
			if order.StoreID.String() != storeID {
				slog.Warn("Stripe webhook: store_id mismatch", "order_id", orderID)
				return nil
			}
		}

		if order.PaymentStatus == "paid" {
			return nil // already processed
		}

		order.PaymentID = session.ID
		order.PaymentStatus = "paid"
		order.Status = "confirmed"
		order.UpdatedAt = time.Now()
		c.DB.UpdateOrder(order)

		slog.Info("Stripe webhook: order confirmed", "order_id", orderID)

	case "checkout.session.expired":
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			return nil
		}

		orderID, ok := session.Metadata["order_id"]
		if !ok || orderID == "" {
			return nil
		}

		order, err := c.DB.GetOrderByID(orderID)
		if err != nil {
			return nil
		}

		if order.PaymentStatus == "paid" {
			return nil // don't override paid status
		}

		order.PaymentStatus = "failed"
		order.Status = "cancelled"
		order.UpdatedAt = time.Now()
		c.DB.UpdateOrder(order)

		slog.Info("Stripe webhook: checkout session expired", "order_id", orderID)
	}

	return nil
}

// GetVariantsByStoreID loads all variants for a store in a single query (N+1 fix).
func (c *Controller) GetVariantsByStoreID(storeID string) ([]models.ProductVariant, error) {
	return c.DB.GetVariantsByStoreID(storeID)
}

// --- Iyzico helper functions ---

// iyzicoBaseURL returns the Iyzico API base URL, using sandbox if configured.
func iyzicoBaseURL(creds map[string]any) string {
	if baseURL, ok := creds["base_url"].(string); ok && baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	if sandbox, ok := creds["sandbox"].(bool); ok && sandbox {
		return "https://sandbox-api.iyzipay.com"
	}
	return "https://api.iyzipay.com"
}

// iyzicoRequest makes a signed HTTP request to the Iyzico API.
func iyzicoRequest(apiKey, secretKey, baseURL, uri string, body []byte) ([]byte, error) {
	// Generate random string for signing
	rnd := make([]byte, 8)
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	for i := range rnd {
		rnd[i] = chars[rand.Intn(len(chars))]
	}
	randomStr := string(rnd)

	// SHA-256 hash of the body
	bodyHash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(bodyHash[:])

	// Build signature: HMAC-SHA256(secretKey, random + uri + bodyHash)
	signInput := randomStr + uri + hashStr
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(signInput))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Authorization header
	authValue := base64.StdEncoding.EncodeToString(
		[]byte(apiKey + ":" + randomStr + ":" + signature))

	// Make HTTP request
	url := baseURL + uri
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "IYZWSv2 "+authValue)
	req.Header.Set("x-iyzi-rnd", randomStr)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
