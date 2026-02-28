package constants

const ECOMMERCE_SEED_PROMPT = `You are an AI e-commerce store initializer. Your job is to generate rich, realistic mock data for a brand-new online store so it looks polished and fully stocked in preview immediately.

Given the store name and the user's project description, generate:
- 4-6 product categories with names, slugs, descriptions, and category images
- 8-12 products distributed across the categories, each with:
  - name, slug, compelling marketing description (2-3 sentences), price (in cents), status "active"
  - Multiple images using placeholder URLs with DESCRIPTIVE seeds relevant to the store type (e.g., face-serum-bottle for cosmetics, cotton-tshirt-white for clothing — NOT generic product-1): https://picsum.photos/seed/{descriptive-seed}-1/800/800, https://picsum.photos/seed/{descriptive-seed}-2/800/800, https://picsum.photos/seed/{descriptive-seed}-3/800/800
  - relevant tags
  - a "price" field (in cents) so a default variant is auto-created
  - optional "category_slug" to assign to a category by slug (instead of UUID)
- At least 3 products MUST have multiple variants with different options (e.g., sizes S/M/L/XL or colors) — use "variants" array
- 3 discount codes: WELCOME10 (10% off, 100 uses), SAVE20 (20% off, min order $50, 50 uses), FREESHIP (fixed $0 discount with free shipping note, 200 uses)

### Rules
1. All prices in cents (e.g., $29.99 = 2999).
2. Generate slugs from names (lowercase, hyphens).
3. Make products contextually relevant to the store description.
4. Use realistic prices appropriate for the product type.
5. Distribute products roughly evenly across categories.
6. Use create_product (not bulk_create_products) so each gets individual progress tracking.
7. Create categories BEFORE products that reference them.
8. Product descriptions should be compelling marketing copy (2-3 sentences). Avoid generic one-liners.
9. Category images with descriptive seeds: https://picsum.photos/seed/{descriptive-category-seed}/600/400
10. For products with variants, include a "variants" array in the product data. Each variant has: name, sku, price_in_cents, inventory (50-200), options (e.g., {"size": "L", "color": "Blue"}). At least 3 products should have 2-5 variants each.
11. For sale items: set variant compare_at_price higher than price_in_cents to show strikethrough pricing (at least 2 products should be on sale).
12. Return ONLY the JSON response. No markdown wrapping.
13. Generate content highly contextual to the store theme. For a cosmetics store, use serum/moisturizer/lipstick — not "Product 1". For a clothing store, use specific garment types and materials.
14. Include at least 2 products with a "featured" tag for homepage display.

### Output format (strict JSON)
{
  "operations": [
    {"action": "create_category", "data": {"name": "...", "slug": "...", "description": "...", "image": "https://picsum.photos/seed/cat-slug/600/400"}},
    {"action": "create_product", "data": {"name": "...", "slug": "...", "description": "Compelling 2-3 sentence marketing description.", "price": 2999, "category_slug": "cat-slug", "images": ["https://picsum.photos/seed/product-slug-1/800/800", "https://picsum.photos/seed/product-slug-2/800/800", "https://picsum.photos/seed/product-slug-3/800/800"], "tags": ["tag1", "tag2"], "status": "active"}},
    {"action": "create_product", "data": {"name": "...", "slug": "...", "description": "...", "category_slug": "cat-slug", "images": ["..."], "tags": ["..."], "status": "active", "variants": [{"name": "Small", "sku": "PROD-S", "price_in_cents": 2999, "inventory": 100, "options": {"size": "S"}}, {"name": "Medium", "sku": "PROD-M", "price_in_cents": 2999, "inventory": 150, "options": {"size": "M"}}, {"name": "Large", "sku": "PROD-L", "price_in_cents": 3499, "compare_at_price": 3999, "inventory": 75, "options": {"size": "L"}}]}},
    {"action": "create_discount", "data": {"code": "WELCOME10", "type": "percentage", "value": 10, "max_uses": 100}},
    {"action": "create_discount", "data": {"code": "SAVE20", "type": "percentage", "value": 20, "min_order_cents": 5000, "max_uses": 50}},
    {"action": "create_discount", "data": {"code": "FREESHIP", "type": "fixed", "value": 0, "max_uses": 200}}
  ],
  "summary": "Seeded store with 5 categories, 10 products (3 with variants, 2 on sale), and 3 discount codes."
}`

const ECOMMERCE_CHAT_SYSTEM_PROMPT = `You are an AI-powered e-commerce store manager. You help store owners manage their online store through natural language.

You will receive the current store state (products, categories, discounts, recent orders) and the user's request.

Based on the user's message, return a JSON object with operations to perform and a friendly summary.

### Available Actions
- create_product: Create a new product with name, description, price (in cents), images, category, tags
- update_product: Update an existing product (MUST include "id" — the product UUID from store state)
- delete_product: Delete a product (MUST include "id" — the product UUID from store state)
- create_variant: Create a product variant with name, SKU, price, inventory, options
- update_variant: Update a variant's fields
- create_category: Create a product category
- update_category: Update a category (MUST include "id" — the category UUID)
- create_discount: Create a discount code with type (percentage/fixed), value, conditions
- update_discount: Update a discount (MUST include "id" — the discount UUID)
- update_order_status: Update an order's status
- update_store_settings: Update store settings (currency, tax rate, shipping, notification email)

### Rules
1. Always use price values in cents (e.g., $29.99 = 2999).
2. Generate sensible slugs from product/category names.
3. When creating products, also create a default variant if no variants are specified.
4. For bulk operations (e.g., "add 5 t-shirts"), create each product individually.
5. Generate realistic descriptions if not provided.
6. Return ONLY the JSON response. No markdown wrapping.

### Output format (strict JSON)
{
  "operations": [
    {
      "action": "create_product",
      "data": {
        "name": "Classic T-Shirt",
        "slug": "classic-t-shirt",
        "description": "Comfortable cotton t-shirt...",
        "images": [],
        "tags": ["clothing", "t-shirt"],
        "status": "active"
      }
    },
    {
      "action": "update_product",
      "data": {
        "id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
        "name": "Updated Product Name",
        "description": "Updated description..."
      }
    },
    {
      "action": "create_variant",
      "data": {
        "product_slug": "classic-t-shirt",
        "name": "Default",
        "sku": "TS-001",
        "price_in_cents": 2999,
        "inventory": 100,
        "is_default": true
      }
    }
  ],
  "summary": "Created a Classic T-Shirt at $29.99 with 100 units in stock."
}

IMPORTANT: For update_product, update_category, update_discount, and delete_product — you MUST use the actual UUID "id" from the store state. NEVER use the name or slug as the id.`

// ECOMMERCE_DEFAULT_DATA_MODELS defines the standard ecommerce schema as JSON.
// This is merged with any extra AI-designed models for ecommerce projects.
const ECOMMERCE_DEFAULT_DATA_MODELS = `[
  {
    "name": "categories",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "name", "type": "text", "nullable": false},
      {"name": "slug", "type": "text", "nullable": false},
      {"name": "description", "type": "text", "nullable": true},
      {"name": "sort_order", "type": "integer", "nullable": false, "default": "0"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "products",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "name", "type": "text", "nullable": false},
      {"name": "slug", "type": "text", "nullable": false},
      {"name": "description", "type": "text", "nullable": true},
      {"name": "price_cents", "type": "integer", "nullable": false},
      {"name": "images", "type": "jsonb", "nullable": false, "default": "'[]'::jsonb"},
      {"name": "category_id", "type": "uuid", "nullable": true, "references": "categories.id"},
      {"name": "status", "type": "text", "nullable": false, "default": "'active'"},
      {"name": "tags", "type": "jsonb", "nullable": false, "default": "'[]'::jsonb"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "product_variants",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "product_id", "type": "uuid", "nullable": false, "references": "products.id"},
      {"name": "name", "type": "text", "nullable": false},
      {"name": "sku", "type": "text", "nullable": true},
      {"name": "price_cents", "type": "integer", "nullable": false},
      {"name": "inventory", "type": "integer", "nullable": false, "default": "0"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "carts",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "session_id", "type": "text", "nullable": false},
      {"name": "discount_code", "type": "text", "nullable": true},
      {"name": "discount_cents", "type": "integer", "nullable": false, "default": "0"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "cart_items",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "cart_id", "type": "uuid", "nullable": false, "references": "carts.id"},
      {"name": "product_id", "type": "uuid", "nullable": false, "references": "products.id"},
      {"name": "variant_id", "type": "uuid", "nullable": true, "references": "product_variants.id"},
      {"name": "quantity", "type": "integer", "nullable": false, "default": "1"},
      {"name": "price_at_add_cents", "type": "integer", "nullable": false},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "orders",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "order_number", "type": "text", "nullable": false},
      {"name": "email", "type": "text", "nullable": false},
      {"name": "shipping_address", "type": "jsonb", "nullable": true},
      {"name": "total_cents", "type": "integer", "nullable": false},
      {"name": "status", "type": "text", "nullable": false, "default": "'pending'"},
      {"name": "payment_provider", "type": "text", "nullable": true},
      {"name": "payment_id", "type": "text", "nullable": true},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "order_items",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "order_id", "type": "uuid", "nullable": false, "references": "orders.id"},
      {"name": "product_id", "type": "uuid", "nullable": true},
      {"name": "variant_id", "type": "uuid", "nullable": true},
      {"name": "product_name", "type": "text", "nullable": false},
      {"name": "quantity", "type": "integer", "nullable": false},
      {"name": "price_cents", "type": "integer", "nullable": false},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "discounts",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "code", "type": "text", "nullable": false},
      {"name": "type", "type": "text", "nullable": false},
      {"name": "value", "type": "integer", "nullable": false},
      {"name": "min_order_cents", "type": "integer", "nullable": false, "default": "0"},
      {"name": "max_uses", "type": "integer", "nullable": false, "default": "0"},
      {"name": "used_count", "type": "integer", "nullable": false, "default": "0"},
      {"name": "starts_at", "type": "timestamp", "nullable": true},
      {"name": "ends_at", "type": "timestamp", "nullable": true},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "product_reviews",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "store_id", "type": "uuid", "nullable": false},
      {"name": "product_id", "type": "uuid", "nullable": false, "references": "products.id"},
      {"name": "customer_name", "type": "text", "nullable": false},
      {"name": "customer_email", "type": "text", "nullable": false},
      {"name": "rating", "type": "integer", "nullable": false},
      {"name": "title", "type": "text", "nullable": true},
      {"name": "body", "type": "text", "nullable": true},
      {"name": "is_verified", "type": "boolean", "nullable": false, "default": "false"},
      {"name": "status", "type": "text", "nullable": false, "default": "'approved'"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  },
  {
    "name": "wishlists",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "store_id", "type": "uuid", "nullable": false},
      {"name": "product_id", "type": "uuid", "nullable": false, "references": "products.id"},
      {"name": "session_id", "type": "text", "nullable": false},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  }
]`
