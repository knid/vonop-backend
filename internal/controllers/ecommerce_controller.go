package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
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
	ctx = utils.WithTokenTracker(ctx, tracker)
	defer func() {
		c.RecordTokenUsage(user.ID, "store_chat", model, provider.Name, store.ID.String(), tracker)
		prompt, completion, total, _ := tracker.Totals()
		if total > 0 {
			credits := models.TokensToCredits(prompt, completion)
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
	case "create_variant":
		return "", c.createVariantFromChat(store, data)
	case "create_variant_with_options":
		return "", c.createVariantWithOptionsFromChat(store, data)
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
		slug = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
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

	product := &models.Product{
		StoreID:     store.ID,
		Name:        name,
		Slug:        slug,
		Description: description,
		Images:      images,
		Tags:        tags,
		Status:      status,
		Metadata:    map[string]any{},
		SEO:         map[string]any{},
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

	product, err := c.DB.CreateProduct(product)
	if err != nil {
		return err
	}

	// Create variants if provided (multi-variant products)
	if variantsRaw, ok := data["variants"].([]any); ok && len(variantsRaw) > 0 {
		for i, vRaw := range variantsRaw {
			vData, ok := vRaw.(map[string]any)
			if !ok {
				continue
			}
			vName, _ := vData["name"].(string)
			vSku, _ := vData["sku"].(string)
			vPrice := 0
			if p, ok := vData["price_in_cents"].(float64); ok {
				vPrice = int(p)
			}
			vInventory := 100
			if inv, ok := vData["inventory"].(float64); ok {
				vInventory = int(inv)
			}
			vCompareAt := 0
			if cp, ok := vData["compare_at_price"].(float64); ok {
				vCompareAt = int(cp)
			}
			vOptions := map[string]any{}
			if opts, ok := vData["options"].(map[string]any); ok {
				vOptions = opts
			}
			c.DB.CreateVariant(&models.ProductVariant{
				ProductID:      product.ID,
				Name:           vName,
				SKU:            vSku,
				PriceInCents:   vPrice,
				CompareAtPrice: vCompareAt,
				Inventory:      vInventory,
				IsDefault:      i == 0,
				Options:        vOptions,
			})
		}
	} else if price, ok := data["price"].(float64); ok {
		// Create default variant if price provided (single-variant product)
		c.DB.CreateVariant(&models.ProductVariant{
			ProductID:    product.ID,
			Name:         "Default",
			PriceInCents: int(price),
			IsDefault:    true,
			Options:      map[string]any{},
		})
	}

	return nil
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

func (c *Controller) createVariantFromChat(store *models.Store, data map[string]any) error {
	productSlug, _ := data["product_slug"].(string)
	productID, _ := data["product_id"].(string)

	var pid uuid.UUID
	if productID != "" {
		pid, _ = uuid.Parse(productID)
	} else if productSlug != "" {
		product, err := c.DB.GetProductBySlug(store.ID.String(), productSlug)
		if err != nil {
			return fmt.Errorf("product not found by slug %q: %w", productSlug, err)
		}
		pid = product.ID
	} else {
		return fmt.Errorf("product_id or product_slug required for variant creation")
	}

	name, _ := data["name"].(string)
	sku, _ := data["sku"].(string)
	price, _ := data["price_in_cents"].(float64)
	inventory, _ := data["inventory"].(float64)
	isDefault, _ := data["is_default"].(bool)

	variant := &models.ProductVariant{
		ProductID:    pid,
		Name:         name,
		SKU:          sku,
		PriceInCents: int(price),
		Inventory:    int(inventory),
		IsDefault:    isDefault,
		Options:      map[string]any{},
	}

	_, err := c.DB.CreateVariant(variant)
	return err
}

func (c *Controller) createVariantWithOptionsFromChat(store *models.Store, data map[string]any) error {
	productID, _ := data["product_id"].(string)
	productSlug, _ := data["product_slug"].(string)

	if productID == "" && productSlug != "" {
		product, err := c.DB.GetProductBySlug(store.ID.String(), productSlug)
		if err != nil {
			return fmt.Errorf("product not found by slug %q: %w", productSlug, err)
		}
		productID = product.ID.String()
	}
	if productID == "" {
		return fmt.Errorf("create_variant_with_options requires product_id or product_slug")
	}

	pid, err := uuid.Parse(productID)
	if err != nil {
		return fmt.Errorf("invalid product_id %q: %w", productID, err)
	}

	name, _ := data["name"].(string)
	if name == "" {
		name = "Default"
	}
	sku, _ := data["sku"].(string)
	priceInCents := chatIntFromAny(data["price_in_cents"])
	inventory := chatIntFromAny(data["inventory"])
	isDefault, _ := data["is_default"].(bool)

	var options map[string]any
	if opts, ok := data["options"].(map[string]any); ok {
		options = opts
	} else {
		options = map[string]any{}
	}

	variant := &models.ProductVariant{
		ProductID:    pid,
		Name:         name,
		SKU:          sku,
		PriceInCents: priceInCents,
		Inventory:    inventory,
		IsDefault:    isDefault,
		Options:      options,
	}

	_, err = c.DB.CreateVariant(variant)
	return err
}

func (c *Controller) bulkCreateProductsFromChat(store *models.Store, data map[string]any) error {
	productsRaw, ok := data["products"].([]any)
	if !ok || len(productsRaw) == 0 {
		return fmt.Errorf("bulk_create_products requires a non-empty products array")
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
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
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

	slug := strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
	slug = slug + "-" + uuid.New().String()[:8]

	var categoryID uuid.UUID
	if req.CategoryID != "" {
		categoryID, _ = uuid.Parse(req.CategoryID)
	}

	product := &models.Product{
		StoreID:     store.ID,
		Name:        req.Name,
		Slug:        slug,
		Description: req.Description,
		Images:      req.Images,
		CategoryID:  categoryID,
		Tags:        req.Tags,
		Status:      "active",
		Metadata:    req.Metadata,
		SEO:         req.SEO,
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

// Variants

func (c *Controller) CreateVariant(productID string, store *models.Store, req requests.CreateVariantRequest) (*models.ProductVariant, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	product, err := c.DB.GetProductByID(productID)
	if err != nil || product.StoreID != store.ID {
		return nil, fmt.Errorf("product not found")
	}

	variant := &models.ProductVariant{
		ProductID:      product.ID,
		Name:           req.Name,
		SKU:            req.SKU,
		PriceInCents:   req.PriceInCents,
		CompareAtPrice: req.CompareAtPrice,
		CostInCents:    req.CostInCents,
		Inventory:      req.Inventory,
		TrackInventory: req.TrackInventory,
		Weight:         req.Weight,
		Options:        req.Options,
		IsDefault:      req.IsDefault,
	}
	if variant.Options == nil {
		variant.Options = map[string]any{}
	}

	return c.DB.CreateVariant(variant)
}

func (c *Controller) GetVariants(productID string) ([]models.ProductVariant, error) {
	return c.DB.GetVariantsByProductID(productID)
}

func (c *Controller) UpdateVariant(variantID string, store *models.Store, req requests.UpdateVariantRequest) (*models.ProductVariant, error) {
	variant, err := c.DB.GetVariantByID(variantID)
	if err != nil {
		return nil, fmt.Errorf("variant not found")
	}
	product, err := c.DB.GetProductByID(variant.ProductID.String())
	if err != nil || product.StoreID != store.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	if req.Name != nil {
		variant.Name = *req.Name
	}
	if req.SKU != nil {
		variant.SKU = *req.SKU
	}
	if req.PriceInCents != nil {
		variant.PriceInCents = *req.PriceInCents
	}
	if req.CompareAtPrice != nil {
		variant.CompareAtPrice = *req.CompareAtPrice
	}
	if req.CostInCents != nil {
		variant.CostInCents = *req.CostInCents
	}
	if req.Inventory != nil {
		variant.Inventory = *req.Inventory
	}
	if req.TrackInventory != nil {
		variant.TrackInventory = *req.TrackInventory
	}
	if req.Weight != nil {
		variant.Weight = *req.Weight
	}
	if req.Options != nil {
		variant.Options = req.Options
	}
	if req.IsDefault != nil {
		variant.IsDefault = *req.IsDefault
	}

	variant.UpdatedAt = time.Now()
	return c.DB.UpdateVariant(variant)
}

func (c *Controller) DeleteVariant(variantID string, store *models.Store) error {
	variant, err := c.DB.GetVariantByID(variantID)
	if err != nil {
		return fmt.Errorf("variant not found")
	}
	product, err := c.DB.GetProductByID(variant.ProductID.String())
	if err != nil || product.StoreID != store.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteVariant(variant)
}

// Categories

func (c *Controller) CreateCategory(store *models.Store, req requests.CreateCategoryRequest) (*models.ProductCategory, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	slug := strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))

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
		cat.Slug = strings.ToLower(strings.ReplaceAll(*req.Name, " ", "-"))
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
