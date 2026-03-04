package views

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82/webhook"
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
// Supports ?category=slug and ?q=search query params
func (v *HttpViews) StorefrontGetProducts(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	if !v.storefrontGuard(w, r) {
		return
	}

	categorySlug := r.URL.Query().Get("category")
	searchQuery := r.URL.Query().Get("q")

	products, err := v.Controller.StorefrontGetProducts(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Load categories for name lookup and optional category filtering
	categories, _ := v.Controller.StorefrontGetCategories(storeID)
	categoryNameMap := make(map[uuid.UUID]string)
	categorySlugMap := make(map[string]uuid.UUID)
	for _, c := range categories {
		categoryNameMap[c.ID] = c.Name
		categorySlugMap[c.Slug] = c.ID
	}

	// Apply category filter
	var categoryFilterID uuid.UUID
	if categorySlug != "" {
		if cid, ok := categorySlugMap[categorySlug]; ok {
			categoryFilterID = cid
		} else {
			// Category slug not found — return empty list
			utils.JSONResponse(w, []responses.StorefrontProductResponse{}, http.StatusOK)
			return
		}
	}

	// Filter products by category and search
	var filtered []models.Product
	for _, p := range products {
		if categoryFilterID != uuid.Nil && p.CategoryID != categoryFilterID {
			continue
		}
		if searchQuery != "" {
			q := strings.ToLower(searchQuery)
			if !strings.Contains(strings.ToLower(p.Name), q) && !strings.Contains(strings.ToLower(p.Description), q) {
				continue
			}
		}
		filtered = append(filtered, p)
	}

	var resp []responses.StorefrontProductResponse
	for _, p := range filtered {
		avgRating, _ := v.Controller.DB.GetAverageRating(p.ID.String())
		reviewCount, _ := v.Controller.DB.GetReviewCount(p.ID.String())
		inStock := !p.TrackInventory || p.Inventory > 0
		resp = append(resp, responses.StorefrontProductResponse{
			ID: p.ID, Name: p.Name, Slug: p.Slug,
			Description: p.Description, Images: p.Images,
			Category: categoryNameMap[p.CategoryID],
			Tags:           p.Tags,
			PriceInCents:   p.PriceInCents,
			CompareAtPrice: p.CompareAtPrice,
			MinPrice:       p.PriceInCents,
			MaxPrice:       p.PriceInCents,
			InStock:        inStock,
			AverageRating:  avgRating, ReviewCount: reviewCount,
		})
	}

	if resp == nil {
		resp = []responses.StorefrontProductResponse{}
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

	product, err := v.Controller.StorefrontGetProduct(storeID, slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	// Resolve category name
	categoryName := ""
	if product.CategoryID != uuid.Nil {
		cat, catErr := v.Controller.StorefrontGetCategoryByID(product.CategoryID.String())
		if catErr == nil {
			categoryName = cat.Name
		}
	}

	avgRating, _ := v.Controller.DB.GetAverageRating(product.ID.String())
	reviewCount, _ := v.Controller.DB.GetReviewCount(product.ID.String())
	inStock := !product.TrackInventory || product.Inventory > 0

	utils.JSONResponse(w, responses.StorefrontProductResponse{
		ID: product.ID, Name: product.Name, Slug: product.Slug,
		Description: product.Description, Images: product.Images,
		Category:       categoryName,
		Tags:           product.Tags,
		PriceInCents:   product.PriceInCents,
		CompareAtPrice: product.CompareAtPrice,
		MinPrice:       product.PriceInCents,
		MaxPrice:       product.PriceInCents,
		InStock:        inStock,
		AverageRating:  avgRating, ReviewCount: reviewCount,
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

// buildCartResponse fetches the full cart state (items + discount) and returns a
// StorefrontCartResponse. Used by GetCart and all cart mutation endpoints so the
// client always receives a consistent, complete cart shape.
func (v *HttpViews) buildCartResponse(storeID, sessionID string) (*responses.StorefrontCartResponse, error) {
	cart, items, err := v.Controller.StorefrontGetCart(storeID, sessionID)
	if err != nil {
		return nil, err
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

		cartItems = append(cartItems, responses.StorefrontCartItem{
			ID: item.ID, ProductID: item.ProductID,
			ProductName: productName, ProductImage: productImage,
			Quantity: item.Quantity, PriceCents: item.PriceAtAddCents,
			TotalCents: total,
		})
	}

	// Resolve discount if applied
	discountCents := 0
	discountCode := ""
	if cart.DiscountID != uuid.Nil {
		discount, dErr := v.Controller.StorefrontGetDiscountByID(cart.DiscountID.String())
		if dErr == nil && discount.IsActive {
			discountCode = discount.Code
			if discount.Type == "percentage" {
				discountCents = subtotal * discount.Value / 100
			} else {
				discountCents = discount.Value
			}
			// Enforce MinOrderCents
			if discount.MinOrderCents > 0 && subtotal < discount.MinOrderCents {
				discountCents = 0
			}
			if discountCents > subtotal {
				discountCents = subtotal
			}
		}
	}

	if cartItems == nil {
		cartItems = []responses.StorefrontCartItem{}
	}

	return &responses.StorefrontCartResponse{
		ID:            cart.ID,
		SessionID:     cart.SessionID,
		Items:         cartItems,
		SubtotalCents: subtotal,
		DiscountCode:  discountCode,
		DiscountCents: discountCents,
		TotalCents:    subtotal - discountCents,
	}, nil
}

// GET /v2/storefront/{storeId}/cart/{sessionId}
func (v *HttpViews) StorefrontGetCart(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	resp, err := v.buildCartResponse(storeID, sessionID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, resp, http.StatusOK)
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
		Quantity  int    `json:"quantity"`
	}
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := v.Controller.StorefrontAddCartItem(storeID, sessionID, req.ProductID, req.Quantity)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackStorefrontEvent(storeID, "storefront_cart_item_added", posthog.NewProperties().
		Set("store_id", storeID).
		Set("product_id", req.ProductID))

	resp, err := v.buildCartResponse(storeID, sessionID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, resp, http.StatusCreated)
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

	resp, cartErr := v.buildCartResponse(storeID, sessionID)
	if cartErr != nil {
		utils.JSONErrorResponse(w, cartErr.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, resp, http.StatusOK)
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

	resp, cartErr := v.buildCartResponse(storeID, sessionID)
	if cartErr != nil {
		utils.JSONErrorResponse(w, cartErr.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, resp, http.StatusOK)
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

	resp, cartErr := v.buildCartResponse(storeID, sessionID)
	if cartErr != nil {
		utils.JSONErrorResponse(w, cartErr.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// DELETE /v2/storefront/{storeId}/cart/{sessionId}/discount
func (v *HttpViews) StorefrontRemoveDiscount(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	if err := v.Controller.StorefrontRemoveDiscount(storeID, sessionID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, cartErr := v.buildCartResponse(storeID, sessionID)
	if cartErr != nil {
		utils.JSONErrorResponse(w, cartErr.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, resp, http.StatusOK)
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

	v.Controller.TrackStorefrontEvent(storeID, "storefront_checkout_started", posthog.NewProperties().
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

	v.Controller.TrackStorefrontEvent(storeID, "store_order_received", posthog.NewProperties().
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

	// Parse event to extract store_id from metadata for signature verification
	var rawEvent struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				Metadata map[string]string `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &rawEvent); err != nil {
		utils.JSONErrorResponse(w, "invalid payload", http.StatusBadRequest)
		return
	}

	storeID := rawEvent.Data.Object.Metadata["store_id"]
	if storeID == "" {
		slog.Warn("Stripe v2 webhook: no store_id in metadata")
		w.WriteHeader(http.StatusOK) // acknowledge but ignore
		return
	}

	store, err := v.Controller.DB.GetStoreByID(storeID)
	if err != nil || store == nil {
		slog.Warn("Stripe v2 webhook: store not found", "store_id", storeID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Verify Stripe signature using the store's webhook secret
	integrations, _ := v.Controller.DB.GetAppIntegrationsByProjectID(store.AppProjectID.String())
	signatureVerified := false
	for _, integ := range integrations {
		if integ.Provider == "stripe" && integ.Status == "connected" {
			var creds map[string]any
			if err := json.Unmarshal(integ.Credentials, &creds); err == nil {
				if webhookSecret, ok := creds["webhook_secret"].(string); ok && webhookSecret != "" {
					sigHeader := r.Header.Get("Stripe-Signature")
					if _, err := webhook.ConstructEvent(body, sigHeader, webhookSecret); err != nil {
						slog.Error("Stripe v2 webhook signature verification failed", "error", err, "store_id", storeID)
						utils.JSONErrorResponse(w, "invalid signature", http.StatusBadRequest)
						return
					}
					signatureVerified = true
				}
			}
			break
		}
	}
	if !signatureVerified {
		slog.Warn("Stripe v2 webhook rejected: no webhook_secret configured", "store_id", storeID)
		utils.JSONErrorResponse(w, "webhook signature verification not configured", http.StatusForbidden)
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

// --- Reviews ---

// GET /v2/storefront/{storeId}/products/{slug}/reviews
func (v *HttpViews) StorefrontGetReviews(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	slug := chi.URLParam(r, "slug")

	if !v.storefrontGuard(w, r) {
		return
	}

	result, err := v.Controller.StorefrontGetReviews(storeID, slug)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/storefront/{storeId}/products/{slug}/reviews
func (v *HttpViews) StorefrontSubmitReview(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	slug := chi.URLParam(r, "slug")

	if !v.storefrontGuard(w, r) {
		return
	}

	var body struct {
		CustomerName  string `json:"customer_name"`
		CustomerEmail string `json:"customer_email"`
		Rating        int    `json:"rating"`
		Title         string `json:"title"`
		Body          string `json:"body"`
	}

	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.CustomerName == "" || body.CustomerEmail == "" || body.Rating == 0 {
		utils.JSONErrorResponse(w, "customer_name, customer_email, and rating are required", http.StatusBadRequest)
		return
	}

	review, err := v.Controller.StorefrontSubmitReview(storeID, slug, body.CustomerName, body.CustomerEmail, body.Rating, body.Title, body.Body)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"id":      review.ID,
		"status":  review.Status,
		"message": "Review submitted successfully",
	}, http.StatusCreated)
}

// --- Wishlist ---

// GET /v2/storefront/{storeId}/wishlist/{sessionId}
func (v *HttpViews) StorefrontGetWishlist(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	items, err := v.Controller.StorefrontGetWishlist(storeID, sessionID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, items, http.StatusOK)
}

// POST /v2/storefront/{storeId}/wishlist/{sessionId}
func (v *HttpViews) StorefrontAddToWishlist(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	if !v.storefrontGuard(w, r) {
		return
	}

	var body struct {
		ProductID string `json:"product_id"`
	}

	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.ProductID == "" {
		utils.JSONErrorResponse(w, "product_id is required", http.StatusBadRequest)
		return
	}

	_, err := v.Controller.StorefrontAddToWishlist(storeID, sessionID, body.ProductID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{"message": "Added to wishlist"}, http.StatusCreated)
}

// DELETE /v2/storefront/{storeId}/wishlist/{sessionId}/{productId}
func (v *HttpViews) StorefrontRemoveFromWishlist(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")
	productID := chi.URLParam(r, "productId")

	if !v.storefrontGuard(w, r) {
		return
	}

	err := v.Controller.StorefrontRemoveFromWishlist(storeID, sessionID, productID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{"message": "Removed from wishlist"}, http.StatusOK)
}
