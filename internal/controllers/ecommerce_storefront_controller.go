package controllers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
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
		return nil, "", "", fmt.Errorf("failed to create order")
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

	// Initiate payment if a payment provider is specified and connected
	paymentURL, clientSecret, err := c.StorefrontInitiatePayment(storeID, order, orderItems)
	if err != nil {
		slog.Warn("Payment initiation failed, order created without payment", "error", err, "order_id", order.ID)
		// Order still exists with pending payment — frontend can retry or use manual payment
	}

	return order, paymentURL, clientSecret, nil
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
	baseURL := fmt.Sprintf("https://%s.wemob.app", store.AppProjectID.String()[:8])
	if project != nil {
		baseURL = fmt.Sprintf("https://%s.wemob.app", project.Slug)
	}

	successURL := baseURL + "/checkout/success?order_id=" + order.ID.String()
	cancelURL := baseURL + "/checkout/cancel?order_id=" + order.ID.String()

	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems:  lineItems,
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
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

func (c *Controller) initiateIyzicoPayment(store *models.Store, order *models.Order, orderItems []models.OrderItem, creds map[string]any) (string, string, error) {
	apiKey, _ := creds["api_key"].(string)
	secretKey, _ := creds["secret_key"].(string)
	if apiKey == "" || secretKey == "" {
		return "", "", fmt.Errorf("iyzico credentials not configured")
	}

	// Iyzico payment form creation
	// Build the payment request body for Iyzico's checkout form API
	project, _ := c.DB.GetAppProjectByID(store.AppProjectID.String())
	baseURL := fmt.Sprintf("https://%s.wemob.app", store.AppProjectID.String()[:8])
	if project != nil {
		baseURL = fmt.Sprintf("https://%s.wemob.app", project.Slug)
	}

	callbackURL := baseURL + "/checkout/callback?order_id=" + order.ID.String()

	// Build basket items for Iyzico
	var basketItems []map[string]any
	for _, item := range orderItems {
		basketItems = append(basketItems, map[string]any{
			"id":               item.ProductID.String(),
			"name":             item.ProductName,
			"category1":        "General",
			"itemType":         "PHYSICAL",
			"price":            fmt.Sprintf("%.2f", float64(item.TotalCents)/100),
		})
	}

	iyzicoPayload := map[string]any{
		"locale":          store.Locale,
		"conversationId":  order.ID.String(),
		"price":           fmt.Sprintf("%.2f", float64(order.SubtotalCents)/100),
		"paidPrice":       fmt.Sprintf("%.2f", float64(order.TotalCents)/100),
		"currency":        order.Currency,
		"basketId":        order.OrderNumber,
		"paymentGroup":    "PRODUCT",
		"callbackUrl":     callbackURL,
		"enabledInstallments": []int{1, 2, 3, 6, 9},
		"buyer": map[string]any{
			"id":                 order.CustomerEmail,
			"name":               order.CustomerName,
			"email":              order.CustomerEmail,
			"registrationAddress": "N/A",
			"city":               "N/A",
			"country":            "N/A",
		},
		"basketItems": basketItems,
	}

	// Store the Iyzico payload in order metadata for webhook reconciliation
	order.Metadata["iyzico_callback_url"] = callbackURL
	order.UpdatedAt = time.Now()
	c.DB.UpdateOrder(order)

	payloadJSON, _ := json.Marshal(iyzicoPayload)
	slog.Info("Iyzico payment form requested", "store_id", store.ID, "order_id", order.ID, "payload_size", len(payloadJSON))

	// Return the client secret (the payload) for the frontend to render the Iyzico form
	// The frontend will submit this to Iyzico's checkout form endpoint
	return "", string(payloadJSON), nil
}
