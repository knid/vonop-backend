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
