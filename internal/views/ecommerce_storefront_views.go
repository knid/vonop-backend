package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/storefront/{storeId}/products
func (v *HttpViews) StorefrontGetProducts(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")

	products, err := v.Controller.StorefrontGetProducts(storeID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.StorefrontProductResponse
	for _, p := range products {
		variants, _ := v.Controller.GetVariants(p.ID.String())
		var sfVariants []responses.StorefrontVariant
		minPrice, maxPrice := 0, 0
		for i, vr := range variants {
			inStock := !vr.TrackInventory || vr.Inventory > 0
			sfVariants = append(sfVariants, responses.StorefrontVariant{
				ID: vr.ID, Name: vr.Name, PriceInCents: vr.PriceInCents,
				CompareAtPrice: vr.CompareAtPrice, Inventory: vr.Inventory,
				InStock: inStock, Options: vr.Options,
			})
			if i == 0 || vr.PriceInCents < minPrice {
				minPrice = vr.PriceInCents
			}
			if vr.PriceInCents > maxPrice {
				maxPrice = vr.PriceInCents
			}
		}

		resp = append(resp, responses.StorefrontProductResponse{
			ID: p.ID, Name: p.Name, Slug: p.Slug,
			Description: p.Description, Images: p.Images,
			Tags: p.Tags, Variants: sfVariants,
			MinPrice: minPrice, MaxPrice: maxPrice,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/storefront/{storeId}/products/{slug}
func (v *HttpViews) StorefrontGetProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	slug := chi.URLParam(r, "slug")

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

	var req requests.CreateCartRequest
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

	var req requests.AddCartItemRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	item, err := v.Controller.StorefrontAddCartItem(storeID, sessionID, req.ProductID, req.VariantID, req.Quantity)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"id":       item.ID,
		"quantity": item.Quantity,
		"price":    item.PriceAtAddCents,
	}, http.StatusCreated)
}

// PUT /v2/storefront/{storeId}/cart/{sessionId}/items/{itemId}
func (v *HttpViews) StorefrontUpdateCartItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")

	var req requests.UpdateCartItemRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := v.Controller.StorefrontUpdateCartItem(itemID, req.Quantity)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// DELETE /v2/storefront/{storeId}/cart/{sessionId}/items/{itemId}
func (v *HttpViews) StorefrontDeleteCartItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")

	if err := v.Controller.StorefrontDeleteCartItem(itemID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// POST /v2/storefront/{storeId}/cart/{sessionId}/discount
func (v *HttpViews) StorefrontApplyDiscount(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeId")
	sessionID := chi.URLParam(r, "sessionId")

	var req requests.ApplyDiscountRequest
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

	var req requests.StorefrontCheckoutRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, paymentURL, clientSecret, err := v.Controller.StorefrontCheckout(storeID, req.SessionID, req.CustomerEmail, req.CustomerName, req.ShippingAddress, req.BillingAddress, req.PaymentProvider)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

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
func (v *HttpViews) StorefrontConfirmCheckout(w http.ResponseWriter, r *http.Request) {
	var req requests.ConfirmCheckoutRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := v.Controller.StorefrontConfirmPayment(req.OrderID, req.PaymentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.CheckoutConfirmResponse{
		SuccessResponse: responses.SuccessResponse{Success: true},
		OrderID:         order.ID,
		OrderNumber:     order.OrderNumber,
		Status:          order.Status,
	}, http.StatusOK)
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
