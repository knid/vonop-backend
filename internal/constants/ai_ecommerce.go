package constants

// ECOMMERCE_STARTER_SCHEMA_DESCRIPTION describes the tables seeded into every
// new ecommerce project's Neon database. Included in the agent's system prompt
// so it can write correct SQL and know what extension points exist.
const ECOMMERCE_STARTER_SCHEMA_DESCRIPTION = `
Every ecommerce project is provisioned with a dedicated Postgres database
(Neon) containing these tables on day one:

- stores (1 row; store configuration)
- product_categories (hierarchical; parent_id self-ref)
- products (references category_id, stores.id)
- discounts (promo codes, percentage or fixed)
- carts (session-scoped) + cart_items
- orders + order_items
- product_reviews
- wishlists

Prices are stored in cents (bigint). All rows have UUID primary keys. Stores,
products, categories, and discounts support soft deletes (deleted_at).

You may add columns or new tables as the user requests. Do not alter core
columns (name, slug, price_in_cents, status) on existing tables — downstream
checkout and listing pages depend on them.
`

const ECOMMERCE_CHAT_SYSTEM_PROMPT = ECOMMERCE_STARTER_SCHEMA_DESCRIPTION + `
You are an AI-powered e-commerce store manager. You help store owners manage their online store through natural language.

You will receive the current store state (products, categories, discounts, recent orders) and the user's request.

Based on the user's message, return a JSON object with operations to perform and a friendly summary.

### Available Actions
- create_product: Create a new product with name, description, price_in_cents, images, category, tags, inventory, sku
- update_product: Update an existing product (MUST include "id" — the product UUID from store state)
- delete_product: Delete a product (MUST include "id" — the product UUID from store state)
- create_category: Create a product category
- update_category: Update a category (MUST include "id" — the category UUID)
- create_discount: Create a discount code with type (percentage/fixed), value, conditions
- update_discount: Update a discount (MUST include "id" — the discount UUID)
- update_order_status: Update an order's status
- update_store_settings: Update store settings (currency, tax rate, shipping, notification email)

### Rules
1. Always use price values in cents (e.g., $29.99 = 2999).
2. Generate sensible slugs from product/category names.
3. Always include price_in_cents when creating products.
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
        "price_in_cents": 2999,
        "inventory": 100,
        "sku": "TS-001",
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
        "price_in_cents": 3499
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
      {"name": "price_in_cents", "type": "integer", "nullable": false, "default": "0"},
      {"name": "compare_at_price", "type": "integer", "nullable": true, "default": "0"},
      {"name": "sku", "type": "text", "nullable": true},
      {"name": "inventory", "type": "integer", "nullable": false, "default": "0"},
      {"name": "track_inventory", "type": "boolean", "nullable": false, "default": "false"},
      {"name": "images", "type": "jsonb", "nullable": false, "default": "'[]'::jsonb"},
      {"name": "category_id", "type": "uuid", "nullable": true, "references": "categories.id"},
      {"name": "status", "type": "text", "nullable": false, "default": "'active'"},
      {"name": "tags", "type": "jsonb", "nullable": false, "default": "'[]'::jsonb"},
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
