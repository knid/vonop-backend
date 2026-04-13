package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// runStoreOp is the shared body every store tool uses. It checks that the
// dispatcher callback is wired, forwards the parsed params, and maps
// dispatcher errors to the agent's (result, isError, err) contract.
//
// Infrastructure failures (nil callback, nil project) return err != nil so
// the run aborts. Validation / lookup failures from the dispatcher come back
// as isError=true so the agent can retry with corrected input.
func runStoreOp(ctx context.Context, rc *RunContext, action string, params map[string]any) (string, bool, error) {
	if rc == nil || rc.Deps == nil || rc.Deps.StoreOp == nil {
		return "store management not available for this project", true, nil
	}
	if rc.Project == nil {
		return "", true, fmt.Errorf("%s: run context is missing project", action)
	}
	result, err := rc.Deps.StoreOp(ctx, rc.Project, action, params)
	if err != nil {
		return fmt.Sprintf("%s failed: %v", action, err), true, nil
	}
	return result, false, nil
}

// -----------------------------------------------------------------------------
// Products
// -----------------------------------------------------------------------------

type ListProductsTool struct{}

func (ListProductsTool) Name() string { return "list_products" }
func (ListProductsTool) Description() string {
	return "List all products in the project's store. Returns a JSON array of product objects with id, slug, name, description, price_in_cents, compare_at_price, inventory, images, tags, and category_id. Takes no parameters. Use this before update_product or delete_product when you don't already know the slug/id."
}
func (ListProductsTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (ListProductsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return runStoreOp(ctx, rc, "list_products", map[string]any{})
}

type CreateProductTool struct{}

func (CreateProductTool) Name() string { return "create_product" }
func (CreateProductTool) Description() string {
	return "Create one product in the store. price_in_cents is required (e.g. $19.99 => 1999). category_slug is optional; if set, that category must already exist (call list_categories or create_category first). images is an array of URLs — call generate_image first to produce them. Lazy-creates the store row on first call."
}
func (CreateProductTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":             map[string]any{"type": "string"},
			"slug":             map[string]any{"type": "string"},
			"description":      map[string]any{"type": "string"},
			"price_in_cents":   map[string]any{"type": "integer"},
			"compare_at_price": map[string]any{"type": "integer"},
			"sku":              map[string]any{"type": "string"},
			"inventory":        map[string]any{"type": "integer"},
			"images":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"category_slug":    map[string]any{"type": "string"},
			"status":           map[string]any{"type": "string", "enum": []string{"active", "draft", "archived"}},
		},
		"required": []string{"name", "price_in_cents"},
	}
}
func (CreateProductTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "create_product", params)
}

type UpdateProductTool struct{}

func (UpdateProductTool) Name() string { return "update_product" }
func (UpdateProductTool) Description() string {
	return "Update fields on an existing product. Provide `id` as the product id, slug, or name to match; all other fields are optional and only supplied fields are changed. Call list_products first if you don't know the id."
}
func (UpdateProductTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string", "description": "product id, slug, or name"},
			"name":             map[string]any{"type": "string"},
			"description":      map[string]any{"type": "string"},
			"status":           map[string]any{"type": "string", "enum": []string{"active", "draft", "archived"}},
			"price_in_cents":   map[string]any{"type": "integer"},
			"compare_at_price": map[string]any{"type": "integer"},
			"sku":              map[string]any{"type": "string"},
			"inventory":        map[string]any{"type": "integer"},
			"images":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"id"},
	}
}
func (UpdateProductTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "update_product", params)
}

type DeleteProductTool struct{}

func (DeleteProductTool) Name() string { return "delete_product" }
func (DeleteProductTool) Description() string {
	return "Archive a product (soft delete). Provide `id` as the product id, slug, or name."
}
func (DeleteProductTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "product id, slug, or name"},
		},
		"required": []string{"id"},
	}
}
func (DeleteProductTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "delete_product", params)
}

type BulkCreateProductsTool struct{}

func (BulkCreateProductsTool) Name() string { return "bulk_create_products" }
func (BulkCreateProductsTool) Description() string {
	return "Create many products in one call. Pass {\"products\": [{...}, {...}]} where each item has the same shape as create_product. If any item references a category_slug that does not exist, the whole call fails fast — call create_category for any missing slugs first."
}
func (BulkCreateProductsTool) Schema() map[string]any {
	productSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":             map[string]any{"type": "string"},
			"slug":             map[string]any{"type": "string"},
			"description":      map[string]any{"type": "string"},
			"price_in_cents":   map[string]any{"type": "integer"},
			"compare_at_price": map[string]any{"type": "integer"},
			"sku":              map[string]any{"type": "string"},
			"inventory":        map[string]any{"type": "integer"},
			"images":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"category_slug":    map[string]any{"type": "string"},
			"status":           map[string]any{"type": "string"},
		},
		"required": []string{"name", "price_in_cents"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"products": map[string]any{
				"type":  "array",
				"items": productSchema,
			},
		},
		"required": []string{"products"},
	}
}
func (BulkCreateProductsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "bulk_create_products", params)
}

// -----------------------------------------------------------------------------
// Categories
// -----------------------------------------------------------------------------

type ListCategoriesTool struct{}

func (ListCategoriesTool) Name() string { return "list_categories" }
func (ListCategoriesTool) Description() string {
	return "List all categories in the store. Returns a JSON array with id, slug, name, description, and image. Takes no parameters."
}
func (ListCategoriesTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (ListCategoriesTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return runStoreOp(ctx, rc, "list_categories", map[string]any{})
}

type CreateCategoryTool struct{}

func (CreateCategoryTool) Name() string { return "create_category" }
func (CreateCategoryTool) Description() string {
	return "Create one product category. The slug is auto-derived from the name if not supplied. The image is optional — call generate_image first to produce a URL."
}
func (CreateCategoryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"slug":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"image":       map[string]any{"type": "string", "description": "image URL, typically from generate_image"},
		},
		"required": []string{"name"},
	}
}
func (CreateCategoryTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "create_category", params)
}

type UpdateCategoryTool struct{}

func (UpdateCategoryTool) Name() string { return "update_category" }
func (UpdateCategoryTool) Description() string {
	return "Update fields on an existing category. The underlying helper currently requires the category UUID in `id` — call list_categories first to look it up."
}
func (UpdateCategoryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string", "description": "category UUID"},
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"sort_order":  map[string]any{"type": "integer"},
		},
		"required": []string{"id"},
	}
}
func (UpdateCategoryTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "update_category", params)
}

type DeleteCategoryTool struct{}

func (DeleteCategoryTool) Name() string { return "delete_category" }
func (DeleteCategoryTool) Description() string {
	return "Delete a category. Requires the category UUID in `id`. Call list_categories first to look it up."
}
func (DeleteCategoryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "category UUID"},
		},
		"required": []string{"id"},
	}
}
func (DeleteCategoryTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "delete_category", params)
}

// -----------------------------------------------------------------------------
// Discounts
// -----------------------------------------------------------------------------

type ListDiscountsTool struct{}

func (ListDiscountsTool) Name() string { return "list_discounts" }
func (ListDiscountsTool) Description() string {
	return "List all discount codes in the store. Returns a JSON array with id, code, type, value, starts_at, ends_at, is_active. Takes no parameters. Call this before update_discount or delete_discount."
}
func (ListDiscountsTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (ListDiscountsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return runStoreOp(ctx, rc, "list_discounts", map[string]any{})
}

type CreateDiscountTool struct{}

func (CreateDiscountTool) Name() string { return "create_discount" }
func (CreateDiscountTool) Description() string {
	return "Create a discount code. `type` is \"percentage\" or \"fixed\". For percentage, `value` is a whole-number percent (20 == 20% off). For fixed, `value` is cents off. starts_at / ends_at are optional RFC3339 timestamps."
}
func (CreateDiscountTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code":            map[string]any{"type": "string"},
			"type":            map[string]any{"type": "string", "enum": []string{"percentage", "fixed"}},
			"value":           map[string]any{"type": "integer"},
			"min_order_cents": map[string]any{"type": "integer"},
			"max_uses":        map[string]any{"type": "integer"},
			"starts_at":       map[string]any{"type": "string", "description": "RFC3339 timestamp"},
			"ends_at":         map[string]any{"type": "string", "description": "RFC3339 timestamp"},
		},
		"required": []string{"code", "type", "value"},
	}
}
func (CreateDiscountTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "create_discount", params)
}

type UpdateDiscountTool struct{}

func (UpdateDiscountTool) Name() string { return "update_discount" }
func (UpdateDiscountTool) Description() string {
	return "Update a discount code. Requires the discount UUID in `id`. Call list_discounts first to look it up."
}
func (UpdateDiscountTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":        map[string]any{"type": "string", "description": "discount UUID"},
			"code":      map[string]any{"type": "string"},
			"is_active": map[string]any{"type": "boolean"},
		},
		"required": []string{"id"},
	}
}
func (UpdateDiscountTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "update_discount", params)
}

type DeleteDiscountTool struct{}

func (DeleteDiscountTool) Name() string { return "delete_discount" }
func (DeleteDiscountTool) Description() string {
	return "Delete a discount code. Requires the discount UUID in `id`. Call list_discounts first to look it up."
}
func (DeleteDiscountTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "discount UUID"},
		},
		"required": []string{"id"},
	}
}
func (DeleteDiscountTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "delete_discount", params)
}

// -----------------------------------------------------------------------------
// Store meta
// -----------------------------------------------------------------------------

type GetStoreTool struct{}

func (GetStoreTool) Name() string { return "get_store" }
func (GetStoreTool) Description() string {
	return "Return the store's current settings as JSON: name, currency, locale, tax_rate, shipping_config, payment_config, notify_email. Takes no parameters. Call this before update_store_settings so you know what values to preserve vs change."
}
func (GetStoreTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (GetStoreTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return runStoreOp(ctx, rc, "get_store", map[string]any{})
}

type UpdateStoreSettingsTool struct{}

func (UpdateStoreSettingsTool) Name() string { return "update_store_settings" }
func (UpdateStoreSettingsTool) Description() string {
	return "Update store settings. All fields are optional — supply only what you want to change. Currency is a 3-letter ISO code (e.g. \"USD\"). tax_rate is a fraction, not a percentage (0.08 == 8%). shipping_config and payment_config are free-form JSON objects; preserve existing keys by calling get_store first and merging client-side."
}
func (UpdateStoreSettingsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":            map[string]any{"type": "string"},
			"currency":        map[string]any{"type": "string"},
			"locale":          map[string]any{"type": "string"},
			"tax_rate":        map[string]any{"type": "number"},
			"shipping_config": map[string]any{"type": "object"},
			"payment_config":  map[string]any{"type": "object"},
			"notify_email":    map[string]any{"type": "string"},
		},
	}
}
func (UpdateStoreSettingsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	return runStoreOp(ctx, rc, "update_store_settings", params)
}
