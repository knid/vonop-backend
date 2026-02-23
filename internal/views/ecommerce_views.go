package views

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// helper to get store from app project
func (v *HttpViews) getStoreForApp(w http.ResponseWriter, r *http.Request) (*models.AppProject, *models.Store, bool) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return nil, nil, false
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return nil, nil, false
	}

	store, err := v.Controller.GetStore(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, "store not found for this project", http.StatusNotFound)
		return nil, nil, false
	}

	return project, store, true
}

// GET /v2/apps/{appId}/store
func (v *HttpViews) GetAppStore(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	utils.JSONResponse(w, responses.StoreResponse{
		ID:             store.ID,
		AppProjectID:   store.AppProjectID,
		StoreName:      store.StoreName,
		Currency:       store.Currency,
		Locale:         store.Locale,
		TaxRate:        store.TaxRate,
		ShippingConfig: store.ShippingConfig,
		PaymentConfig:  store.PaymentConfig,
		NotifyEmail:    store.NotifyEmail,
		Status:         store.Status,
		CreatedAt:      store.CreatedAt,
		UpdatedAt:      store.UpdatedAt,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}/store/settings
func (v *HttpViews) UpdateAppStoreSettings(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	var req requests.UpdateStoreSettingsRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	store, err := v.Controller.UpdateStoreSettings(store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.StoreResponse{
		ID:             store.ID,
		AppProjectID:   store.AppProjectID,
		StoreName:      store.StoreName,
		Currency:       store.Currency,
		Locale:         store.Locale,
		TaxRate:        store.TaxRate,
		ShippingConfig: store.ShippingConfig,
		PaymentConfig:  store.PaymentConfig,
		NotifyEmail:    store.NotifyEmail,
		Status:         store.Status,
		CreatedAt:      store.CreatedAt,
		UpdatedAt:      store.UpdatedAt,
	}, http.StatusOK)
}

// POST /v2/apps/{appId}/store/chat
func (v *HttpViews) ChatStoreManagement(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	store, err := v.Controller.GetStore(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, "store not found", http.StatusNotFound)
		return
	}

	var req requests.StoreChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "store_chat_sent", posthog.NewProperties().
		Set("store_id", store.ID.String()))

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "store_chat",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			defer close(events)
			response, streamErr := v.Controller.StreamChatStoreManagement(ctx, store, user, req.Message, func(chunk string) {
				events <- responses.SSEEvent{Event: "chat_thinking", Data: responses.ChatThinkingEvent{Chunk: chunk}}
			})
			if streamErr != nil {
				events <- responses.SSEEvent{Event: "error", Data: responses.GenerationErrorEvent{Error: streamErr.Error(), Phase: "store_chat"}}
				return
			}
			events <- responses.SSEEvent{Event: "done", Data: map[string]string{"response": response}}
		},
	})
}

// Products CRUD

// GET /v2/apps/{appId}/store/products
func (v *HttpViews) GetStoreProducts(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	products, err := v.Controller.GetProducts(store.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.ProductResponse
	for _, p := range products {
		variants, _ := v.Controller.GetVariants(p.ID.String())
		var variantResp []responses.ProductVariantResp
		for _, vr := range variants {
			variantResp = append(variantResp, responses.ProductVariantResp{
				ID: vr.ID, Name: vr.Name, SKU: vr.SKU,
				PriceInCents: vr.PriceInCents, CompareAtPrice: vr.CompareAtPrice,
				CostInCents: vr.CostInCents, Inventory: vr.Inventory,
				TrackInventory: vr.TrackInventory, Weight: vr.Weight,
				Options: vr.Options, IsDefault: vr.IsDefault,
			})
		}
		resp = append(resp, responses.ProductResponse{
			ID: p.ID, StoreID: p.StoreID, Name: p.Name, Slug: p.Slug,
			Description: p.Description, Images: p.Images, CategoryID: p.CategoryID,
			Tags: p.Tags, Status: p.Status, Variants: variantResp,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/store/products
func (v *HttpViews) CreateStoreProduct(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	var req requests.CreateProductRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	product, err := v.Controller.CreateProduct(store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if u, authErr := v.Controller.GetUserFromReq(r); authErr == nil {
		v.Controller.TrackUserEvent(u, "store_product_created", posthog.NewProperties().
			Set("store_id", store.ID.String()).
			Set("product_name", product.Name))
	}

	utils.JSONResponse(w, responses.ProductResponse{
		ID: product.ID, StoreID: product.StoreID, Name: product.Name,
		Slug: product.Slug, Description: product.Description,
		Images: product.Images, Tags: product.Tags, Status: product.Status,
		CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
	}, http.StatusCreated)
}

// GET /v2/apps/{appId}/store/products/{productId}
func (v *HttpViews) GetStoreProduct(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	productID := chi.URLParam(r, "productId")
	product, err := v.Controller.GetProductByID(productID)
	if err != nil || product.StoreID != store.ID {
		utils.JSONErrorResponse(w, "product not found", http.StatusNotFound)
		return
	}

	variants, _ := v.Controller.GetVariants(product.ID.String())
	var variantResp []responses.ProductVariantResp
	for _, vr := range variants {
		variantResp = append(variantResp, responses.ProductVariantResp{
			ID: vr.ID, Name: vr.Name, SKU: vr.SKU,
			PriceInCents: vr.PriceInCents, CompareAtPrice: vr.CompareAtPrice,
			Inventory: vr.Inventory, Options: vr.Options, IsDefault: vr.IsDefault,
		})
	}

	utils.JSONResponse(w, responses.ProductResponse{
		ID: product.ID, StoreID: product.StoreID, Name: product.Name,
		Slug: product.Slug, Description: product.Description,
		Images: product.Images, Tags: product.Tags, Status: product.Status,
		Variants: variantResp,
		CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}/store/products/{productId}
func (v *HttpViews) UpdateStoreProduct(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	productID := chi.URLParam(r, "productId")
	var req requests.UpdateProductRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	product, err := v.Controller.UpdateProduct(productID, store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.ProductResponse{
		ID: product.ID, Name: product.Name, Slug: product.Slug,
		Status: product.Status, CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/store/products/{productId}
func (v *HttpViews) DeleteStoreProduct(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	productID := chi.URLParam(r, "productId")
	if err := v.Controller.DeleteProduct(productID, store); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// Variants

// POST /v2/apps/{appId}/store/products/{productId}/variants
func (v *HttpViews) CreateProductVariant(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	productID := chi.URLParam(r, "productId")
	var req requests.CreateVariantRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	variant, err := v.Controller.CreateVariant(productID, store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.ProductVariantResp{
		ID: variant.ID, Name: variant.Name, SKU: variant.SKU,
		PriceInCents: variant.PriceInCents, IsDefault: variant.IsDefault,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/store/products/{productId}/variants/{variantId}
func (v *HttpViews) UpdateProductVariant(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	variantID := chi.URLParam(r, "variantId")
	var req requests.UpdateVariantRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	variant, err := v.Controller.UpdateVariant(variantID, store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.ProductVariantResp{
		ID: variant.ID, Name: variant.Name, PriceInCents: variant.PriceInCents,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/store/products/{productId}/variants/{variantId}
func (v *HttpViews) DeleteProductVariant(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	variantID := chi.URLParam(r, "variantId")
	if err := v.Controller.DeleteVariant(variantID, store); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// Categories

// GET /v2/apps/{appId}/store/categories
func (v *HttpViews) GetStoreCategories(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	categories, err := v.Controller.GetCategories(store.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.CategoryResponse
	for _, c := range categories {
		resp = append(resp, responses.CategoryResponse{
			ID: c.ID, Name: c.Name, Slug: c.Slug, ParentID: c.ParentID,
			Image: c.Image, Description: c.Description, SortOrder: c.SortOrder,
			CreatedAt: c.CreatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/store/categories
func (v *HttpViews) CreateStoreCategory(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	var req requests.CreateCategoryRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cat, err := v.Controller.CreateCategory(store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.CategoryResponse{
		ID: cat.ID, Name: cat.Name, Slug: cat.Slug, CreatedAt: cat.CreatedAt,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/store/categories/{categoryId}
func (v *HttpViews) UpdateStoreCategory(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	categoryID := chi.URLParam(r, "categoryId")
	var req requests.UpdateCategoryRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	cat, err := v.Controller.UpdateCategory(categoryID, store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.CategoryResponse{
		ID: cat.ID, Name: cat.Name, Slug: cat.Slug, CreatedAt: cat.CreatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/store/categories/{categoryId}
func (v *HttpViews) DeleteStoreCategory(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	categoryID := chi.URLParam(r, "categoryId")
	if err := v.Controller.DeleteCategory(categoryID, store); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// Discounts

// GET /v2/apps/{appId}/store/discounts
func (v *HttpViews) GetStoreDiscounts(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	discounts, err := v.Controller.GetDiscounts(store.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.DiscountResponse
	for _, d := range discounts {
		resp = append(resp, responses.DiscountResponse{
			ID: d.ID, Code: d.Code, Type: d.Type, Value: d.Value,
			MinOrderCents: d.MinOrderCents, MaxUses: d.MaxUses,
			UsedCount: d.UsedCount, IsActive: d.IsActive,
			CreatedAt: d.CreatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// POST /v2/apps/{appId}/store/discounts
func (v *HttpViews) CreateStoreDiscount(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	var req requests.CreateDiscountRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	discount, err := v.Controller.CreateDiscount(store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if u, authErr := v.Controller.GetUserFromReq(r); authErr == nil {
		v.Controller.TrackUserEvent(u, "store_discount_created", posthog.NewProperties().
			Set("store_id", store.ID.String()))
	}

	utils.JSONResponse(w, responses.DiscountResponse{
		ID: discount.ID, Code: discount.Code, Type: discount.Type,
		Value: discount.Value, IsActive: discount.IsActive, CreatedAt: discount.CreatedAt,
	}, http.StatusCreated)
}

// PUT /v2/apps/{appId}/store/discounts/{discountId}
func (v *HttpViews) UpdateStoreDiscount(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	discountID := chi.URLParam(r, "discountId")
	var req requests.UpdateDiscountRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	discount, err := v.Controller.UpdateDiscount(discountID, store, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.DiscountResponse{
		ID: discount.ID, Code: discount.Code, IsActive: discount.IsActive,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/store/discounts/{discountId}
func (v *HttpViews) DeleteStoreDiscount(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	discountID := chi.URLParam(r, "discountId")
	if err := v.Controller.DeleteDiscount(discountID, store); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

// Orders

// GET /v2/apps/{appId}/store/orders
func (v *HttpViews) GetStoreOrders(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	orders, err := v.Controller.GetOrders(store.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resp []responses.OrderResponse
	for _, o := range orders {
		resp = append(resp, responses.OrderResponse{
			ID: o.ID, OrderNumber: o.OrderNumber, CustomerEmail: o.CustomerEmail,
			CustomerName: o.CustomerName, TotalCents: o.TotalCents,
			Currency: o.Currency, Status: o.Status, PaymentStatus: o.PaymentStatus,
			CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
		})
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}

// GET /v2/apps/{appId}/store/orders/{orderId}
func (v *HttpViews) GetStoreOrder(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	orderID := chi.URLParam(r, "orderId")
	order, err := v.Controller.GetOrder(orderID, store)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	items, _ := v.Controller.GetOrderItems(order.ID.String())
	var itemResp []responses.OrderItemResp
	for _, i := range items {
		itemResp = append(itemResp, responses.OrderItemResp{
			ID: i.ID, ProductName: i.ProductName, VariantName: i.VariantName,
			Quantity: i.Quantity, PriceInCents: i.PriceInCents, TotalCents: i.TotalCents,
		})
	}

	utils.JSONResponse(w, responses.OrderResponse{
		ID: order.ID, OrderNumber: order.OrderNumber,
		CustomerEmail: order.CustomerEmail, CustomerName: order.CustomerName,
		ShippingAddress: order.ShippingAddress,
		SubtotalCents: order.SubtotalCents, DiscountCents: order.DiscountCents,
		TaxCents: order.TaxCents, ShippingCents: order.ShippingCents,
		TotalCents: order.TotalCents, Currency: order.Currency,
		Status: order.Status, PaymentStatus: order.PaymentStatus,
		PaymentProvider: order.PaymentProvider, Items: itemResp,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}/store/orders/{orderId}
func (v *HttpViews) UpdateStoreOrderStatus(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	orderID := chi.URLParam(r, "orderId")
	var req requests.UpdateOrderStatusRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := v.Controller.UpdateOrderStatus(orderID, store, req.Status)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.OrderResponse{
		ID: order.ID, OrderNumber: order.OrderNumber,
		Status: order.Status, UpdatedAt: order.UpdatedAt,
	}, http.StatusOK)
}

// Analytics

// GET /v2/apps/{appId}/store/analytics
func (v *HttpViews) GetStoreAnalytics(w http.ResponseWriter, r *http.Request) {
	_, store, ok := v.getStoreForApp(w, r)
	if !ok {
		return
	}

	analytics, err := v.Controller.GetStoreAnalytics(store)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, analytics, http.StatusOK)
}
