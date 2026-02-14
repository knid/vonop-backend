package views

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// storefrontGuard checks that the store exists and is active. Returns false if rejected.
func (v *HttpViews) storefrontGuard(w http.ResponseWriter, r *http.Request) bool {
	storeID := chi.URLParam(r, "storeId")
	_, err := v.Controller.StorefrontGuard(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return false
	}
	return true
}

// GET /v2/storefront/{storeId}/products
// Fix 16: N+1 query fix — loads all variants in a single query
func (v *HttpViews) StorefrontGetProducts(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	if !v.storefrontGuard(w, r) {
		return
	}

	products, err := v.Controller.StorefrontGetProducts(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Load all variants at once to avoid N+1 queries
	allVariants, _ := v.Controller.GetVariantsByStoreID(storeID)
	variantMap := make(map[uuid.UUID][]responses.StorefrontVariant)
	minPriceMap := make(map[uuid.UUID]int)
	maxPriceMap := make(map[uuid.UUID]int)
	for _, vr := range allVariants {
		inStock := !vr.TrackInventory || vr.Inventory > 0
		sfVariant := responses.StorefrontVariant{
			ID: vr.ID, Name: vr.Name, PriceInCents: vr.PriceInCents,
			CompareAtPrice: vr.CompareAtPrice, Inventory: vr.Inventory,
			InStock: inStock, Options: vr.Options,
		}
		variantMap[vr.ProductID] = append(variantMap[vr.ProductID], sfVariant)

		if cur, ok := minPriceMap[vr.ProductID]; !ok || vr.PriceInCents < cur {
			minPriceMap[vr.ProductID] = vr.PriceInCents
		}
		if cur, ok := maxPriceMap[vr.ProductID]; !ok || vr.PriceInCents > cur {
			maxPriceMap[vr.ProductID] = vr.PriceInCents
		}
	}

	var resp []responses.StorefrontProductResponse
	for _, p := range products {
		resp = append(resp, responses.StorefrontProductResponse{
			ID: p.ID, Name: p.Name, Slug: p.Slug,
			Description: p.Description, Images: p.Images,
			Tags: p.Tags, Variants: variantMap[p.ID],
			MinPrice: minPriceMap[p.ID], MaxPrice: maxPriceMap[p.ID],
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/storefront/{storeId}/products/{slug}
func (v *HttpViews) StorefrontGetProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	slug := chi.URLParam(r, "slug")

	if !v.storefrontGuard(w, r) {
		return
	}

	product, variants, err := v.Controller.StorefrontGetProduct(storeID, slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var sfVariants []responses.StorefrontVariant
	for _, vr := range variants {
		inStock := !vr.TrackInventory || vr.Inventory > 0
		sfVariants = append(sfVariants, responses.StorefrontVariant{
			ID: vr.ID, Name: vr.Name, PriceInCents: vr.PriceInCents,
			CompareAtPrice: vr.CompareAtPrice, Inventory: vr.Inventory,
			InStock: inStock, Options: vr.Options,
		})
	}

	utils.JSONResponse(w, responses.StorefrontProductResponse{
		ID: product.ID, Name: product.Name, Slug: product.Slug,
		Description: product.Description, Images: product.Images,
		Tags: product.Tags, Variants: sfVariants,
	}, http.StatusOK)
}

// GET /v2/storefront/{storeId}/categories
func (v *HttpViews) StorefrontGetCategories(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	if !v.storefrontGuard(w, r) {
		return
	}

	categories, err := v.Controller.StorefrontGetCategories(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.StorefrontCategoryResponse
	for _, c := range categories {
		resp = append(resp, responses.StorefrontCategoryResponse{
			ID: c.ID, Name: c.Name, Slug: c.Slug,
			Image: c.Image, Description: c.Description,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/storefront/{storeId}/cart
func (v *HttpViews) StorefrontCreateCart(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cart, err := v.Controller.StorefrontCreateCart(storeID, req.SessionID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, responses.StorefrontCartResponse{
		ID:            cart.ID,
		SessionID:     cart.SessionID,
		Items:         []responses.StorefrontCartItem{},
		SubtotalCents: 0,
		TotalCents:    0,
	}, http.StatusCreated)
}

// GET /v2/storefront/{storeId}/cart/{sessionId}
func (v *HttpViews) StorefrontGetCart(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	cart, items, err := v.Controller.StorefrontGetCart(storeID, sessionID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	subtotal := 0
	var cartItems []responses.StorefrontCartItem
	for _, item := range items {
		total := item.PriceAtAddCents * item.Quantity
		subtotal += total

		productName := ""
		productImage := ""
		if item.Product != nil {
			productName = item.Product.Name
			if len(item.Product.Images) > 0 {
				productImage = item.Product.Images[0]
			}
		}
		variantName := ""
		if item.Variant != nil {
			variantName = item.Variant.Name
		}

		cartItems = append(cartItems, responses.StorefrontCartItem{
			ID: item.ID, ProductID: item.ProductID,
			ProductName: productName, ProductImage: productImage,
			VariantID: item.VariantID, VariantName: variantName,
			Quantity: item.Quantity, PriceCents: item.PriceAtAddCents,
			TotalCents: total,
		})
	}

	utils.JSONResponse(w, responses.StorefrontCartResponse{
		ID:            cart.ID,
		SessionID:     cart.SessionID,
		Items:         cartItems,
		SubtotalCents: subtotal,
		TotalCents:    subtotal,
	}, http.StatusOK)
}

// POST /v2/storefront/{storeId}/cart/{sessionId}/items
func (v *HttpViews) StorefrontAddCartItem(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var req struct {
		ProductID string `json:"product_id"`
		VariantID string `json:"variant_id"`
		Quantity  int    `json:"quantity"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	item, err := v.Controller.StorefrontAddCartItem(storeID, sessionID, req.ProductID, req.VariantID, req.Quantity)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackEvent(uuid.Nil, "storefront_cart_item_added", posthog.NewProperties().
		Set("store_id", storeID).
		Set("product_id", req.ProductID))

	utils.JSONResponse(w, map[string]any{
		"id":       item.ID,
		"quantity": item.Quantity,
		"price":    item.PriceAtAddCents,
	}, http.StatusCreated)
}

// PUT /v2/storefront/{storeId}/cart/{sessionId}/items/{itemId}
// Fix 2: passes storeID and sessionID for IDOR verification
func (v *HttpViews) StorefrontUpdateCartItem(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")
	itemID := chi.URLParam(r, "itemId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var req struct {
		Quantity int `json:"quantity"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := v.Controller.StorefrontUpdateCartItem(storeID, sessionID, itemID, req.Quantity)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// DELETE /v2/storefront/{storeId}/cart/{sessionId}/items/{itemId}
// Fix 2: passes storeID and sessionID for IDOR verification
func (v *HttpViews) StorefrontDeleteCartItem(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")
	itemID := chi.URLParam(r, "itemId")

	if !v.storefrontGuard(w, r) {
		return
	}

	if err := v.Controller.StorefrontDeleteCartItem(storeID, sessionID, itemID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// POST /v2/storefront/{storeId}/cart/{sessionId}/discount
func (v *HttpViews) StorefrontApplyDiscount(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := v.Controller.StorefrontApplyDiscount(storeID, sessionID, req.Code)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// POST /v2/storefront/{storeId}/checkout
func (v *HttpViews) StorefrontCheckout(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var req struct {
		SessionID       string         `json:"session_id"`
		CustomerEmail   string         `json:"customer_email"`
		CustomerName    string         `json:"customer_name"`
		ShippingAddress map[string]any `json:"shipping_address"`
		BillingAddress  map[string]any `json:"billing_address"`
		PaymentProvider string         `json:"payment_provider"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, paymentURL, clientSecret, err := v.Controller.StorefrontCheckout(storeID, req.SessionID, req.CustomerEmail, req.CustomerName, req.ShippingAddress, req.BillingAddress, req.PaymentProvider)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackEvent(uuid.Nil, "storefront_checkout_started", posthog.NewProperties().
		Set("store_id", storeID).
		Set("cart_total", order.TotalCents))

	utils.JSONResponse(w, responses.StorefrontCheckoutResponse{
		OrderID:      order.ID,
		OrderNumber:  order.OrderNumber,
		TotalCents:   order.TotalCents,
		Currency:     order.Currency,
		PaymentURL:   paymentURL,
		ClientSecret: clientSecret,
	}, http.StatusCreated)
}

// POST /v2/storefront/{storeId}/checkout/confirm
// Fix 1: passes storeID for server-side payment verification
func (v *HttpViews) StorefrontConfirmCheckout(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	var req struct {
		OrderID   string `json:"order_id"`
		PaymentID string `json:"payment_id"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := v.Controller.StorefrontConfirmPayment(storeID, req.OrderID, req.PaymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackEvent(uuid.Nil, "store_order_received", posthog.NewProperties().
		Set("store_id", storeID).
		Set("order_total", order.TotalCents))

	utils.JSONResponse(w, responses.CheckoutConfirmResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		OrderID:         order.ID,
		OrderNumber:     order.OrderNumber,
		Status:          order.Status,
	}, http.StatusOK)
}

// POST /v2/storefront/{storeId}/checkout/iyzico-callback
// Fix 14: Iyzico callback endpoint
func (v *HttpViews) StorefrontIyzicoCallback(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	orderID := r.URL.Query().Get("order_id")

	// Iyzico sends the token as a form value
	token := r.FormValue("token")
	if token == "" {
		// Try JSON body as fallback
		var req struct {
			Token string `json:"token"`
		}
		utils.ParseJSONBody(r, &req)
		token = req.Token
	}

	if orderID == "" || token == "" {
		utils.JSONErrorResponse(w, "missing order_id or token", http.StatusBadRequest)
		return
	}

	redirectURL, err := v.Controller.StorefrontHandleIyzicoCallback(storeID, orderID, token)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// POST /v2/storefront/webhooks/stripe
// Fix 15: Stripe webhook for per-store accounts
func (v *HttpViews) StorefrontStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	if err := v.Controller.StorefrontHandleStripeWebhook(body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// GET /v2/storefront/{storeId}/config
func (v *HttpViews) StorefrontGetStoreConfig(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	store, paymentProviders, err := v.Controller.StorefrontGetStoreConfig(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	shippingEnabled := len(store.ShippingConfig) > 0
	if enabled, ok := store.ShippingConfig["enabled"].(bool); ok {
		shippingEnabled = enabled
	}

	utils.JSONResponse(w, responses.StoreConfigResponse{
		Name:             store.StoreName,
		Currency:         store.Currency,
		Locale:           store.Locale,
		TaxRate:          store.TaxRate,
		PaymentProviders: paymentProviders,
		ShippingEnabled:  shippingEnabled,
	}, http.StatusOK)
}

// GET /v2/storefront/{storeId}/orders/{orderId}
func (v *HttpViews) StorefrontGetOrder(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	orderID := chi.URLParam(r, "orderId")

	order, items, err := v.Controller.StorefrontGetOrder(storeID, orderID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var respItems []responses.OrderTrackingItemResponse
	for _, item := range items {
		respItems = append(respItems, responses.OrderTrackingItemResponse{
			ProductName:  item.ProductName,
			VariantName:  item.VariantName,
			Quantity:     item.Quantity,
			PriceInCents: item.PriceInCents,
			TotalCents:   item.TotalCents,
		})
	}

	utils.JSONResponse(w, responses.OrderTrackingResponse{
		OrderID:       order.ID,
		OrderNumber:   order.OrderNumber,
		Status:        order.Status,
		PaymentStatus: order.PaymentStatus,
		Items:         respItems,
		SubtotalCents: order.SubtotalCents,
		DiscountCents: order.DiscountCents,
		TaxCents:      order.TaxCents,
		TotalCents:    order.TotalCents,
		Currency:      order.Currency,
		CreatedAt:     order.CreatedAt,
	}, http.StatusOK)
}
