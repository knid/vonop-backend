package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/utils"
)

// CreateStore creates an e-commerce store for an app project
func (c *Controller) CreateStore(project *models.AppProject, user *models.User, storeName string) (*models.Store, error) {
	store := &models.Store{
		AppProjectID:   project.ID,
		UserID:         user.ID,
		StoreName:      storeName,
		Currency:       "USD",
		Locale:         "en",
		TaxRate:        0,
		ShippingConfig: map[string]any{},
		PaymentConfig:  map[string]any{},
		Status:         "active",
	}
	store, err := c.DB.CreateStore(store)
	if err != nil {
		return nil, err
	}

	// Persist ecommerce env vars so they appear in dashboard and are always available
	c.SetAppEnvVar(project, requests.SetEnvVarRequest{
		Key: "NEXT_PUBLIC_STORE_ID", Value: store.ID.String(), IsPublic: true,
	})
	c.SetAppEnvVar(project, requests.SetEnvVarRequest{
		Key: "NEXT_PUBLIC_WEMOB_API_URL", Value: constants.WEMOB_API_BASE_URL, IsPublic: true,
	})

	return store, nil
}

// GetStore returns the store for a project
func (c *Controller) GetStore(projectID string) (*models.Store, error) {
	return c.DB.GetStoreByAppProjectID(projectID)
}

// UpdateStoreSettings updates store configuration
func (c *Controller) UpdateStoreSettings(store *models.Store, req requests.UpdateStoreSettingsRequest) (*models.Store, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	if req.StoreName != "" {
		store.StoreName = req.StoreName
	}
	if req.Currency != "" {
		store.Currency = req.Currency
	}
	if req.Locale != "" {
		store.Locale = req.Locale
	}
	if req.TaxRate != nil {
		store.TaxRate = *req.TaxRate
	}
	if req.ShippingConfig != nil {
		store.ShippingConfig = req.ShippingConfig
	}
	if req.PaymentConfig != nil {
		store.PaymentConfig = req.PaymentConfig
	}
	if req.NotifyEmail != "" {
		store.NotifyEmail = req.NotifyEmail
	}

	store.UpdatedAt = time.Now()
	return c.DB.UpdateStore(store)
}

// StreamChatStoreManagement streams store management chat
func (c *Controller) StreamChatStoreManagement(ctx context.Context, store *models.Store, user *models.User, message string, onChunk func(chunk string)) (string, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	// Token usage tracking
	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true
	tracker.UserID = user.ID.String()
	tracker.ProjectID = store.AppProjectID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user, "store_chat", model, provider.Name, store.ID.String(), "ecommerce", tracker)
		prompt, completion, total, _ := tracker.Totals()
		if total > 0 {
			credits := c.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, "store_chat", store.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits from tokens", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
		}
	}()

	products, _ := c.DB.GetProductsByStoreID(store.ID.String())
	categories, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
	discounts, _ := c.DB.GetDiscountsByStoreID(store.ID.String())

	storeContext := map[string]any{
		"store":      store,
		"products":   products,
		"categories": categories,
		"discounts":  discounts,
	}
	contextJSON, _ := json.Marshal(storeContext)

	llmMessages := []utils.Message{
		{Role: "system", Content: constants.ECOMMERCE_CHAT_SYSTEM_PROMPT},
		{Role: "system", Content: fmt.Sprintf("Current store state:\n%s", string(contextJSON))},
		{Role: "user", Content: message},
	}

	response, err := utils.StreamJSONFromLLMWithMessages(ctx, model, llmMessages, provider, onChunk, nil)
	if err != nil {
		return "", err
	}

	// Execute operations after streaming completes
	var result struct {
		Operations []struct {
			Action string         `json:"action"`
			Data   map[string]any `json:"data"`
		} `json:"operations"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		for _, op := range result.Operations {
			_, _ = c.executeStoreOperation(store, op.Action, op.Data)
		}
	}

	return response, nil
}

// Fix 18/23: executeStoreOperation returns (data string, error) so list operations
// can surface results to the LLM tool caller.
func (c *Controller) executeStoreOperation(store *models.Store, action string, data map[string]any) (string, error) {
	switch action {
	case "create_product":
		return "", c.createProductFromChat(store, data)
	case "update_product":
		return "", c.updateProductFromChat(store, data)
	case "delete_product":
		return "", c.deleteProductFromChat(store, data)
	case "bulk_create_products":
		return "", c.bulkCreateProductsFromChat(store, data)
	case "create_category":
		return "", c.createCategoryFromChat(store, data)
	case "update_category":
		return "", c.updateCategoryFromChat(data)
	case "delete_category":
		return "", c.deleteCategoryFromChat(data)
	case "create_discount":
		return "", c.createDiscountFromChat(store, data)
	case "update_discount":
		return "", c.updateDiscountFromChat(data)
	case "delete_discount":
		return "", c.deleteDiscountFromChat(data)
	case "update_store_settings":
		return "", c.updateStoreFromChat(store, data)
	case "list_products":
		products, err := c.DB.GetProductsByStoreID(store.ID.String())
		if err != nil {
			return "", err
		}
		result, _ := json.Marshal(products)
		return string(result), nil
	case "list_categories":
		categories, err := c.DB.GetCategoriesByStoreID(store.ID.String())
		if err != nil {
			return "", err
		}
		result, _ := json.Marshal(categories)
		return string(result), nil
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (c *Controller) createProductFromChat(store *models.Store, data map[string]any) error {
	name, _ := data["name"].(string)
	slug, _ := data["slug"].(string)
	if slug == "" {
		slug = utils.Slugify(name)
	}
	// Fix 17: Ensure slug uniqueness like the direct API does
	slug = slug + "-" + uuid.New().String()[:8]
	description, _ := data["description"].(string)
	status := "active"
	if s, ok := data["status"].(string); ok {
		status = s
	}

	var images []string
	if imgs, ok := data["images"].([]any); ok {
		for _, img := range imgs {
			if s, ok := img.(string); ok {
				images = append(images, s)
			}
		}
	}

	var tags []string
	if t, ok := data["tags"].([]any); ok {
		for _, tag := range t {
			if s, ok := tag.(string); ok {
				tags = append(tags, s)
			}
		}
	}

	// Extract price fields
	priceInCents := 0
	if p, ok := data["price_in_cents"].(float64); ok {
		priceInCents = int(p)
	} else if p, ok := data["price"].(float64); ok {
		priceInCents = int(p)
	}
	// Backward compat: if price was in a "variants" array, use first variant's price
	if priceInCents == 0 {
		if variantsRaw, ok := data["variants"].([]any); ok && len(variantsRaw) > 0 {
			if vData, ok := variantsRaw[0].(map[string]any); ok {
				if p, ok := vData["price_in_cents"].(float64); ok {
					priceInCents = int(p)
				}
			}
		}
	}
	compareAtPrice := 0
	if cp, ok := data["compare_at_price"].(float64); ok {
		compareAtPrice = int(cp)
	}
	sku, _ := data["sku"].(string)
	inventory := 0
	if inv, ok := data["inventory"].(float64); ok {
		inventory = int(inv)
	}

	product := &models.Product{
		StoreID:        store.ID,
		Name:           name,
		Slug:           slug,
		Description:    description,
		Images:         images,
		Tags:           tags,
		Status:         status,
		PriceInCents:   priceInCents,
		CompareAtPrice: compareAtPrice,
		SKU:            sku,
		Inventory:      inventory,
		Metadata:       map[string]any{},
		SEO:            map[string]any{},
	}

	// Look up category by slug if category_slug is provided
	if catSlug, ok := data["category_slug"].(string); ok && catSlug != "" {
		categories, err := c.DB.GetCategoriesByStoreID(store.ID.String())
		if err == nil {
			for _, cat := range categories {
				if cat.Slug == catSlug {
					product.CategoryID = cat.ID
					break
				}
			}
		}
	}

	_, err := c.DB.CreateProduct(product)
	return err
}

func (c *Controller) findProductByAnyIdentifier(store *models.Store, data map[string]any) (*models.Product, error) {
	idStr, _ := data["id"].(string)

	// 1. Try UUID lookup
	if _, parseErr := uuid.Parse(idStr); parseErr == nil {
		if p, err := c.DB.GetProductByID(idStr); err == nil {
			return p, nil
		}
	}

	// 2. Try slug lookup (from explicit slug field or from id field)
	slug, _ := data["slug"].(string)
	candidates := []string{slug, idStr}
	for _, s := range candidates {
		if s == "" {
			continue
		}
		if p, err := c.DB.GetProductBySlug(store.ID.String(), s); err == nil {
			return p, nil
		}
	}

	// 3. Last resort: search by name among store products
	products, err := c.DB.GetProductsByStoreID(store.ID.String())
	if err != nil {
		return nil, fmt.Errorf("product not found: %s", idStr)
	}
	nameQuery := strings.ToLower(idStr)
	for i := range products {
		if strings.ToLower(products[i].Name) == nameQuery || strings.ToLower(products[i].Slug) == nameQuery {
			return &products[i], nil
		}
	}

	return nil, fmt.Errorf("product not found: %s", idStr)
}

func (c *Controller) updateProductFromChat(store *models.Store, data map[string]any) error {
	product, err := c.findProductByAnyIdentifier(store, data)
	if err != nil {
		return err
	}
	if name, ok := data["name"].(string); ok {
		product.Name = name
	}
	if desc, ok := data["description"].(string); ok {
		product.Description = desc
	}
	if status, ok := data["status"].(string); ok {
		product.Status = status
	}
	if p, ok := data["price_in_cents"].(float64); ok {
		product.PriceInCents = int(p)
	}
	if cp, ok := data["compare_at_price"].(float64); ok {
		product.CompareAtPrice = int(cp)
	}
	if s, ok := data["sku"].(string); ok {
		product.SKU = s
	}
	if inv, ok := data["inventory"].(float64); ok {
		product.Inventory = int(inv)
	}
	if imgs, ok := data["images"].([]any); ok {
		var images []string
		for _, img := range imgs {
			if s, ok := img.(string); ok {
				images = append(images, s)
			}
		}
		product.Images = images
	}
	if tags, ok := data["tags"].([]any); ok {
		var t []string
		for _, tag := range tags {
			if s, ok := tag.(string); ok {
				t = append(t, s)
			}
		}
		product.Tags = t
	}
	product.UpdatedAt = time.Now()
	_, err = c.DB.UpdateProduct(product)
	return err
}

func (c *Controller) deleteProductFromChat(store *models.Store, data map[string]any) error {
	product, err := c.findProductByAnyIdentifier(store, data)
	if err != nil {
		return err
	}
	product.Status = "archived"
	product.UpdatedAt = time.Now()
	_, err = c.DB.UpdateProduct(product)
	return err
}

func (c *Controller) bulkCreateProductsFromChat(store *models.Store, data map[string]any) error {
	productsRaw, ok := data["products"].([]any)
	if !ok || len(productsRaw) == 0 {
		return fmt.Errorf("bulk_create_products requires data.products to be a non-empty array. Correct format: {\"products\": [{\"name\": \"...\", \"price_in_cents\": 999, ...}]}. Do NOT put products directly in data — nest them under the \"products\" key")
	}

	var errs []string
	for i, raw := range productsRaw {
		pData, ok := raw.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("product[%d]: invalid format", i))
			continue
		}
		if err := c.createProductFromChat(store, pData); err != nil {
			name, _ := pData["name"].(string)
			if name == "" {
				name = fmt.Sprintf("index %d", i)
			}
			errs = append(errs, fmt.Sprintf("product %q: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("bulk_create_products had %d/%d failures: %s",
			len(errs), len(productsRaw), strings.Join(errs, "; "))
	}
	return nil
}

func (c *Controller) updateCategoryFromChat(data map[string]any) error {
	id, _ := data["id"].(string)
	if id == "" {
		return fmt.Errorf("update_category requires id")
	}
	cat, err := c.DB.GetCategoryByID(id)
	if err != nil {
		return fmt.Errorf("category %q not found: %w", id, err)
	}
	if name, ok := data["name"].(string); ok && name != "" {
		cat.Name = name
	}
	if desc, ok := data["description"].(string); ok {
		cat.Description = desc
	}
	if _, ok := data["sort_order"]; ok {
		cat.SortOrder = chatIntFromAny(data["sort_order"])
	}
	cat.UpdatedAt = time.Now()
	_, err = c.DB.UpdateCategory(cat)
	return err
}

func (c *Controller) deleteCategoryFromChat(data map[string]any) error {
	id, _ := data["id"].(string)
	if id == "" {
		return fmt.Errorf("delete_category requires id")
	}
	cat, err := c.DB.GetCategoryByID(id)
	if err != nil {
		return fmt.Errorf("category %q not found: %w", id, err)
	}
	return c.DB.DeleteCategory(cat)
}

func (c *Controller) deleteDiscountFromChat(data map[string]any) error {
	id, _ := data["id"].(string)
	if id == "" {
		return fmt.Errorf("delete_discount requires id")
	}
	discount, err := c.DB.GetDiscountByID(id)
	if err != nil {
		return fmt.Errorf("discount %q not found: %w", id, err)
	}
	return c.DB.DeleteDiscount(discount)
}

// chatIntFromAny extracts an int from a value that may be float64 (from JSON) or int.
func chatIntFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func (c *Controller) createCategoryFromChat(store *models.Store, data map[string]any) error {
	name, _ := data["name"].(string)
	slug := utils.Slugify(name)
	if s, ok := data["slug"].(string); ok {
		slug = s
	}
	description, _ := data["description"].(string)

	image, _ := data["image"].(string)

	category := &models.ProductCategory{
		StoreID:     store.ID,
		Name:        name,
		Slug:        slug,
		Description: description,
		Image:       image,
	}

	_, err := c.DB.CreateCategory(category)
	return err
}

func (c *Controller) createDiscountFromChat(store *models.Store, data map[string]any) error {
	code, _ := data["code"].(string)
	discType, _ := data["type"].(string)
	value, _ := data["value"].(float64)
	minOrder, _ := data["min_order_cents"].(float64)
	maxUses, _ := data["max_uses"].(float64)

	discount := &models.Discount{
		StoreID:       store.ID,
		Code:          strings.ToUpper(code),
		Type:          discType,
		Value:         int(value),
		MinOrderCents: int(minOrder),
		MaxUses:       int(maxUses),
		IsActive:      true,
		AppliesTo:     map[string]any{},
	}

	// Fix 19: Parse StartsAt/EndsAt timestamps
	if startsAt, ok := data["starts_at"].(string); ok && startsAt != "" {
		if t, err := time.Parse(time.RFC3339, startsAt); err == nil {
			discount.StartsAt = t
		}
	}
	if endsAt, ok := data["ends_at"].(string); ok && endsAt != "" {
		if t, err := time.Parse(time.RFC3339, endsAt); err == nil {
			discount.EndsAt = t
		}
	}

	_, err := c.DB.CreateDiscount(discount)
	return err
}

func (c *Controller) updateDiscountFromChat(data map[string]any) error {
	idStr, _ := data["id"].(string)
	discount, err := c.DB.GetDiscountByID(idStr)
	if err != nil {
		return err
	}
	if code, ok := data["code"].(string); ok {
		discount.Code = code
	}
	if isActive, ok := data["is_active"].(bool); ok {
		discount.IsActive = isActive
	}
	discount.UpdatedAt = time.Now()
	_, err = c.DB.UpdateDiscount(discount)
	return err
}

// GenerateProductImages generates AI product images for all products in a store
// that don't already have images. Best-effort: logs failures but never returns an error.
func (c *Controller) GenerateProductImages(ctx context.Context, store *models.Store) {
	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		slog.Info("ecommerce-gen: FAL_API_KEY not set, skipping product image generation")
		return
	}

	model := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if model == "" {
		model = "fal-ai/flux/schnell"
	}

	products, err := c.DB.GetProductsByStoreID(store.ID.String())
	if err != nil {
		slog.Warn("ecommerce-gen: failed to fetch products for image generation", "error", err)
		return
	}

	// Filter to products without images
	var needImages []models.Product
	for _, p := range products {
		if len(p.Images) == 0 {
			needImages = append(needImages, p)
		}
	}
	if len(needImages) == 0 {
		return
	}

	slog.Info("ecommerce-gen: generating product images",
		"store_id", store.ID, "count", len(needImages), "model", model)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // max 5 concurrent

	for i := range needImages {
		wg.Add(1)
		go func(product models.Product) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			prompt := buildProductImagePrompt(product.Name, product.Description)

			cdnURL, err := fal.GenerateImage(ctx, apiKey, model, prompt, "")
			if err != nil {
				slog.Warn("ecommerce-gen: fal image generation failed",
					"product_id", product.ID, "error", err)
				return
			}

			imgData, err := downloadImage(ctx, cdnURL)
			if err != nil {
				slog.Warn("ecommerce-gen: failed to download generated image",
					"product_id", product.ID, "error", err)
				return
			}

			s3Key := fmt.Sprintf("stores/%s/products/%s/cover.jpg", store.ID, product.ID)
			if err := c.AssetsStorage.UploadFileByteWithContentType(ctx, s3Key, imgData, "image/jpeg"); err != nil {
				slog.Warn("ecommerce-gen: failed to upload product image to S3",
					"product_id", product.ID, "error", err)
				return
			}

			// Store the S3 key — presigned URLs are generated on-demand when serving
			product.Images = []string{s3Key}
			product.UpdatedAt = time.Now()
			if _, err := c.DB.UpdateProduct(&product); err != nil {
				slog.Warn("ecommerce-gen: failed to update product images",
					"product_id", product.ID, "error", err)
			}
		}(needImages[i])
	}

	wg.Wait()
	slog.Info("ecommerce-gen: product image generation complete", "store_id", store.ID)
}

func buildProductImagePrompt(name, description string) string {
	prompt := name
	if description != "" {
		prompt += " — " + description
	}
	prompt += ". Professional product photo, clean white background, studio lighting, e-commerce photography."
	return prompt
}

var imageDownloadClient = &http.Client{Timeout: 30 * time.Second}

func downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := imageDownloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Controller) updateStoreFromChat(store *models.Store, data map[string]any) error {
	if currency, ok := data["currency"].(string); ok {
		store.Currency = currency
	}
	if taxRate, ok := data["tax_rate"].(float64); ok {
		store.TaxRate = taxRate
	}
	if email, ok := data["notify_email"].(string); ok {
		store.NotifyEmail = email
	}
	store.UpdatedAt = time.Now()
	_, err := c.DB.UpdateStore(store)
	return err
}

// CRUD operations for direct API access

func (c *Controller) CreateProduct(store *models.Store, req requests.CreateProductRequest) (*models.Product, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	slug := utils.Slugify(req.Name) + "-" + uuid.New().String()[:8]

	var categoryID uuid.UUID
	if req.CategoryID != "" {
		categoryID, _ = uuid.Parse(req.CategoryID)
	}

	product := &models.Product{
		StoreID:        store.ID,
		Name:           req.Name,
		Slug:           slug,
		Description:    req.Description,
		Images:         req.Images,
		CategoryID:     categoryID,
		Tags:           req.Tags,
		Status:         "active",
		PriceInCents:   req.PriceInCents,
		CompareAtPrice: req.CompareAtPrice,
		SKU:            req.SKU,
		Inventory:      req.Inventory,
		TrackInventory: req.TrackInventory,
		Metadata:       req.Metadata,
		SEO:            req.SEO,
	}
	if req.Status != "" {
		product.Status = req.Status
	}
	if product.Images == nil {
		product.Images = []string{}
	}
	if product.Tags == nil {
		product.Tags = []string{}
	}
	if product.Metadata == nil {
		product.Metadata = map[string]any{}
	}
	if product.SEO == nil {
		product.SEO = map[string]any{}
	}

	return c.DB.CreateProduct(product)
}

func (c *Controller) UpdateProduct(productID string, store *models.Store, req requests.UpdateProductRequest) (*models.Product, error) {
	product, err := c.DB.GetProductByID(productID)
	if err != nil {
		return nil, fmt.Errorf("product not found")
	}
	if product.StoreID != store.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	if req.Name != nil {
		product.Name = *req.Name
	}
	if req.Description != nil {
		product.Description = *req.Description
	}
	if req.Images != nil {
		product.Images = req.Images
	}
	if req.Tags != nil {
		product.Tags = req.Tags
	}
	if req.Status != nil {
		product.Status = *req.Status
	}
	if req.Metadata != nil {
		product.Metadata = req.Metadata
	}
	if req.SEO != nil {
		product.SEO = req.SEO
	}
	if req.CategoryID != nil {
		cid, _ := uuid.Parse(*req.CategoryID)
		product.CategoryID = cid
	}
	if req.PriceInCents != nil {
		product.PriceInCents = *req.PriceInCents
	}
	if req.CompareAtPrice != nil {
		product.CompareAtPrice = *req.CompareAtPrice
	}
	if req.SKU != nil {
		product.SKU = *req.SKU
	}
	if req.Inventory != nil {
		product.Inventory = *req.Inventory
	}
	if req.TrackInventory != nil {
		product.TrackInventory = *req.TrackInventory
	}

	product.UpdatedAt = time.Now()
	return c.DB.UpdateProduct(product)
}

func (c *Controller) DeleteProduct(productID string, store *models.Store) error {
	product, err := c.DB.GetProductByID(productID)
	if err != nil {
		return fmt.Errorf("product not found")
	}
	if product.StoreID != store.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteProduct(product)
}

func (c *Controller) GetProducts(storeID string) ([]models.Product, error) {
	return c.DB.GetProductsByStoreID(storeID)
}

func (c *Controller) GetProductByID(id string) (*models.Product, error) {
	return c.DB.GetProductByID(id)
}

// Categories

func (c *Controller) CreateCategory(store *models.Store, req requests.CreateCategoryRequest) (*models.ProductCategory, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	slug := utils.Slugify(req.Name)

	var parentID uuid.UUID
	if req.ParentID != "" {
		parentID, _ = uuid.Parse(req.ParentID)
	}

	category := &models.ProductCategory{
		StoreID:     store.ID,
		Name:        req.Name,
		Slug:        slug,
		ParentID:    parentID,
		Image:       req.Image,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}

	return c.DB.CreateCategory(category)
}

func (c *Controller) GetCategories(storeID string) ([]models.ProductCategory, error) {
	return c.DB.GetCategoriesByStoreID(storeID)
}

func (c *Controller) UpdateCategory(categoryID string, store *models.Store, req requests.UpdateCategoryRequest) (*models.ProductCategory, error) {
	cat, err := c.DB.GetCategoryByID(categoryID)
	if err != nil {
		return nil, fmt.Errorf("category not found")
	}
	if cat.StoreID != store.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	if req.Name != nil {
		cat.Name = *req.Name
		cat.Slug = utils.Slugify(*req.Name)
	}
	if req.Image != nil {
		cat.Image = *req.Image
	}
	if req.Description != nil {
		cat.Description = *req.Description
	}
	if req.SortOrder != nil {
		cat.SortOrder = *req.SortOrder
	}

	cat.UpdatedAt = time.Now()
	return c.DB.UpdateCategory(cat)
}

func (c *Controller) DeleteCategory(categoryID string, store *models.Store) error {
	cat, err := c.DB.GetCategoryByID(categoryID)
	if err != nil {
		return fmt.Errorf("category not found")
	}
	if cat.StoreID != store.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteCategory(cat)
}

// Discounts

func (c *Controller) CreateDiscount(store *models.Store, req requests.CreateDiscountRequest) (*models.Discount, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	discount := &models.Discount{
		StoreID:       store.ID,
		Code:          strings.ToUpper(req.Code),
		Type:          req.Type,
		Value:         req.Value,
		MinOrderCents: req.MinOrderCents,
		MaxUses:       req.MaxUses,
		IsActive:      true,
		AppliesTo:     req.AppliesTo,
	}
	if discount.AppliesTo == nil {
		discount.AppliesTo = map[string]any{}
	}

	// Fix 19: Parse StartsAt/EndsAt timestamps
	if req.StartsAt != "" {
		if t, err := time.Parse(time.RFC3339, req.StartsAt); err == nil {
			discount.StartsAt = t
		}
	}
	if req.EndsAt != "" {
		if t, err := time.Parse(time.RFC3339, req.EndsAt); err == nil {
			discount.EndsAt = t
		}
	}

	return c.DB.CreateDiscount(discount)
}

func (c *Controller) GetDiscounts(storeID string) ([]models.Discount, error) {
	return c.DB.GetDiscountsByStoreID(storeID)
}

func (c *Controller) UpdateDiscount(discountID string, store *models.Store, req requests.UpdateDiscountRequest) (*models.Discount, error) {
	discount, err := c.DB.GetDiscountByID(discountID)
	if err != nil {
		return nil, fmt.Errorf("discount not found")
	}
	if discount.StoreID != store.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	if req.Code != nil {
		discount.Code = strings.ToUpper(*req.Code)
	}
	if req.Type != nil {
		discount.Type = *req.Type
	}
	if req.Value != nil {
		discount.Value = *req.Value
	}
	if req.MinOrderCents != nil {
		discount.MinOrderCents = *req.MinOrderCents
	}
	if req.MaxUses != nil {
		discount.MaxUses = *req.MaxUses
	}
	if req.IsActive != nil {
		discount.IsActive = *req.IsActive
	}
	if req.AppliesTo != nil {
		discount.AppliesTo = req.AppliesTo
	}

	discount.UpdatedAt = time.Now()
	return c.DB.UpdateDiscount(discount)
}

func (c *Controller) DeleteDiscount(discountID string, store *models.Store) error {
	discount, err := c.DB.GetDiscountByID(discountID)
	if err != nil {
		return fmt.Errorf("discount not found")
	}
	if discount.StoreID != store.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteDiscount(discount)
}

// ExecuteStoreOp is the single dispatcher invoked by every store-management
// agent tool. It looks up (or lazy-creates) the project's store row, then
// routes on `action` to the existing *FromChat helpers or direct DB calls.
// Returns a JSON string for list/get actions and a short human-readable
// message for mutations. Per-action errors surface as error returns; the
// tool layer converts them into isError=true results the agent can retry.
func (c *Controller) ExecuteStoreOp(
	ctx context.Context,
	project *models.AppProject,
	user *models.User,
	action string,
	params map[string]any,
) (string, error) {
	if project == nil {
		return "", fmt.Errorf("execute_store_op: project is required")
	}

	// Lazy store creation — first call for a project creates the row using
	// the project name as a default store name. Subsequent calls find it.
	store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
	if err != nil || store == nil || store.ID == uuid.Nil {
		if user == nil {
			return "", fmt.Errorf("execute_store_op: cannot lazy-create store without user")
		}
		created, createErr := c.CreateStore(project, user, project.Name)
		if createErr != nil {
			return "", fmt.Errorf("lazy-create store: %w", createErr)
		}
		store = created
	}

	switch action {
	// -------- Products --------
	case "list_products":
		products, err := c.DB.GetProductsByStoreID(store.ID.String())
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(products)
		return string(b), nil

	case "create_product":
		if err := c.createProductFromChat(store, params); err != nil {
			return "", err
		}
		name, _ := params["name"].(string)
		return fmt.Sprintf("created product %q", name), nil

	case "update_product":
		if err := c.updateProductFromChat(store, params); err != nil {
			return "", err
		}
		return "product updated", nil

	case "delete_product":
		if err := c.deleteProductFromChat(store, params); err != nil {
			return "", err
		}
		return "product archived", nil

	case "bulk_create_products":
		// Fail-fast preflight: if any item references a category_slug, that
		// slug must exist in the store's categories before we start inserting.
		productsRaw, ok := params["products"].([]any)
		if !ok || len(productsRaw) == 0 {
			return "", fmt.Errorf("bulk_create_products requires params.products to be a non-empty array")
		}
		cats, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
		knownSlugs := make(map[string]struct{}, len(cats))
		for _, cat := range cats {
			knownSlugs[cat.Slug] = struct{}{}
		}
		for i, raw := range productsRaw {
			pData, ok := raw.(map[string]any)
			if !ok {
				return "", fmt.Errorf("bulk_create_products: item %d is not an object", i)
			}
			if slug, ok := pData["category_slug"].(string); ok && slug != "" {
				if _, exists := knownSlugs[slug]; !exists {
					return "", fmt.Errorf("bulk_create_products: category_slug %q does not exist; create it first with create_category", slug)
				}
			}
		}
		if err := c.bulkCreateProductsFromChat(store, params); err != nil {
			return "", err
		}
		return fmt.Sprintf("created %d products", len(productsRaw)), nil

	// -------- Categories --------
	case "list_categories":
		cats, err := c.DB.GetCategoriesByStoreID(store.ID.String())
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(cats)
		return string(b), nil

	case "create_category":
		if err := c.createCategoryFromChat(store, params); err != nil {
			return "", err
		}
		name, _ := params["name"].(string)
		return fmt.Sprintf("created category %q", name), nil

	case "update_category":
		if err := c.updateCategoryFromChat(params); err != nil {
			return "", err
		}
		return "category updated", nil

	case "delete_category":
		if err := c.deleteCategoryFromChat(params); err != nil {
			return "", err
		}
		return "category deleted", nil

	// -------- Discounts --------
	case "list_discounts":
		discounts, err := c.DB.GetDiscountsByStoreID(store.ID.String())
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(discounts)
		return string(b), nil

	case "create_discount":
		if err := c.createDiscountFromChat(store, params); err != nil {
			return "", err
		}
		code, _ := params["code"].(string)
		return fmt.Sprintf("created discount %q", code), nil

	case "update_discount":
		if err := c.updateDiscountFromChat(params); err != nil {
			return "", err
		}
		return "discount updated", nil

	case "delete_discount":
		if err := c.deleteDiscountFromChat(params); err != nil {
			return "", err
		}
		return "discount deleted", nil

	// -------- Store meta --------
	case "get_store":
		b, _ := json.Marshal(store)
		return string(b), nil

	case "update_store_settings":
		if err := c.updateStoreFromChat(store, params); err != nil {
			return "", err
		}
		// updateStoreFromChat only handles currency / tax_rate / notify_email.
		// Name / locale / shipping_config / payment_config are updated here
		// so the tool surface matches the spec without modifying the legacy
		// helper (which is still used by StreamChatStoreManagement).
		dirty := false
		if name, ok := params["name"].(string); ok && name != "" {
			store.StoreName = name
			dirty = true
		}
		if locale, ok := params["locale"].(string); ok && locale != "" {
			store.Locale = locale
			dirty = true
		}
		if shipping, ok := params["shipping_config"].(map[string]any); ok {
			store.ShippingConfig = shipping
			dirty = true
		}
		if payment, ok := params["payment_config"].(map[string]any); ok {
			store.PaymentConfig = payment
			dirty = true
		}
		if dirty {
			store.UpdatedAt = time.Now()
			if _, err := c.DB.UpdateStore(store); err != nil {
				return "", err
			}
		}
		return "store settings updated", nil

	default:
		return "", fmt.Errorf("execute_store_op: unknown action %q", action)
	}
}
