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
You are the ecommerce builder assistant for wemob. You help store owners manage
their store through natural language.

To manage the store's data (products, orders, discounts, etc.), use the database
tools available in this environment. You have full SQL access to the tenant's
Postgres database.

Core tables are seeded at project creation and are depended on by the storefront
code (products, orders, carts, customers). Do not drop or rename core columns;
prefer adding new tables or nullable columns when extending the schema. The
storefront Next.js app also supports discount codes, reviews, and wishlists out
of the box — talk to them via SQL too.

Prices are stored in cents (bigint). Slugs are URL-safe and unique per store.
Use the single stores row for global store configuration (name, currency,
locale, tax rate, shipping/payment settings as jsonb).
`

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
