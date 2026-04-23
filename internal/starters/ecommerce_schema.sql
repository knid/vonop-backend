-- Canonical ecommerce starter schema. Seeded once per tenant Neon DB on project
-- provisioning. After seeding, this file is frozen — the AI owns all subsequent
-- schema changes per tenant.
--
-- Source of truth: this file mirrors the Go models at internal/models/ecommerce.go
-- at the time of authoring. After PR 1, this SQL diverges intentionally per
-- tenant and the Go models become deletion targets in PR 5.
--
-- Type mappings (Bun postgres dialect):
--   Go int              -> bigint
--   Go int32            -> integer
--   Go float64          -> double precision
--   Go string           -> text
--   Go bool             -> boolean
--   Go uuid.UUID        -> uuid
--   Go time.Time        -> timestamptz
--   Go map[string]any   -> jsonb
--   Go []string         -> jsonb
--
-- Notes:
--   * stores.app_project_id and stores.user_id reference rows in the main
--     platform DB (not this tenant DB), so they are plain uuid columns without
--     foreign keys here.
--   * All other cross-table references stay within this tenant DB and are FKs.
--   * Soft-delete is modelled via a nullable deleted_at column on tables whose
--     Go struct declares a ,soft_delete field (Store, Product, ProductCategory,
--     Discount). Tables without that field (Cart, CartItem, Order, OrderItem,
--     ProductReview, Wishlist) do not get deleted_at.

BEGIN;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- stores (one row per tenant DB; app_project_id is informational — the DB is
-- already scoped to one tenant)
CREATE TABLE stores (
    id               uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_project_id   uuid NOT NULL UNIQUE,
    user_id          uuid NOT NULL,
    store_name       text NOT NULL,
    currency         text NOT NULL DEFAULT 'USD',
    locale           text NOT NULL DEFAULT 'en',
    tax_rate         double precision NOT NULL DEFAULT 0,
    shipping_config  jsonb NOT NULL DEFAULT '{}'::jsonb,
    payment_config   jsonb NOT NULL DEFAULT '{}'::jsonb,
    notify_email     text,
    status           text NOT NULL DEFAULT 'active',
    created_at       timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at       timestamptz NOT NULL DEFAULT current_timestamp,
    deleted_at       timestamptz
);

-- product_categories
CREATE TABLE product_categories (
    id           uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id     uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    name         text NOT NULL,
    slug         text NOT NULL,
    parent_id    uuid REFERENCES product_categories(id) ON DELETE SET NULL,
    image        text,
    description  text,
    sort_order   bigint NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at   timestamptz NOT NULL DEFAULT current_timestamp,
    deleted_at   timestamptz,
    UNIQUE (store_id, slug)
);
CREATE INDEX idx_product_categories_store ON product_categories(store_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_product_categories_parent ON product_categories(parent_id) WHERE deleted_at IS NULL;

-- products
CREATE TABLE products (
    id                uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id          uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    name              text NOT NULL,
    slug              text NOT NULL,
    description       text,
    images            jsonb NOT NULL DEFAULT '[]'::jsonb,
    category_id       uuid REFERENCES product_categories(id) ON DELETE SET NULL,
    tags              jsonb NOT NULL DEFAULT '[]'::jsonb,
    status            text NOT NULL DEFAULT 'active',
    price_in_cents    bigint NOT NULL DEFAULT 0,
    compare_at_price  bigint DEFAULT 0,
    sku               text,
    inventory         bigint NOT NULL DEFAULT 0,
    track_inventory   boolean NOT NULL DEFAULT false,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    seo               jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at        timestamptz NOT NULL DEFAULT current_timestamp,
    deleted_at        timestamptz,
    UNIQUE (store_id, slug)
);
CREATE INDEX idx_products_store_status ON products(store_id, status) WHERE deleted_at IS NULL;
CREATE INDEX idx_products_category ON products(category_id) WHERE deleted_at IS NULL;

-- discounts
CREATE TABLE discounts (
    id               uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id         uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    code             text NOT NULL,
    type             text NOT NULL,               -- 'percentage' | 'fixed'
    value            bigint NOT NULL,             -- percentage (0-100) or fixed amount in cents
    min_order_cents  bigint NOT NULL DEFAULT 0,
    max_uses         bigint NOT NULL DEFAULT 0,   -- 0 = unlimited
    used_count       bigint NOT NULL DEFAULT 0,
    applies_to       jsonb NOT NULL DEFAULT '{}'::jsonb,
    starts_at        timestamptz,
    ends_at          timestamptz,
    is_active        boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at       timestamptz NOT NULL DEFAULT current_timestamp,
    deleted_at       timestamptz,
    UNIQUE (store_id, code)
);
CREATE INDEX idx_discounts_store_active ON discounts(store_id, is_active) WHERE deleted_at IS NULL;

-- carts
CREATE TABLE carts (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id        uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    session_id      text NOT NULL,
    customer_email  text,
    discount_id     uuid REFERENCES discounts(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at      timestamptz NOT NULL DEFAULT current_timestamp
);
CREATE INDEX idx_carts_session ON carts(store_id, session_id);

-- cart_items
CREATE TABLE cart_items (
    id                 uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    cart_id            uuid NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
    product_id         uuid NOT NULL REFERENCES products(id),
    quantity           bigint NOT NULL DEFAULT 1,
    price_at_add_cents bigint NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at         timestamptz NOT NULL DEFAULT current_timestamp
);
CREATE INDEX idx_cart_items_cart ON cart_items(cart_id);

-- orders
CREATE TABLE orders (
    id                uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id          uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    order_number      text NOT NULL UNIQUE,
    customer_email    text NOT NULL,
    customer_name     text NOT NULL,
    shipping_address  jsonb NOT NULL DEFAULT '{}'::jsonb,
    billing_address   jsonb NOT NULL DEFAULT '{}'::jsonb,
    subtotal_cents    bigint NOT NULL,
    discount_cents    bigint NOT NULL DEFAULT 0,
    tax_cents         bigint NOT NULL DEFAULT 0,
    shipping_cents    bigint NOT NULL DEFAULT 0,
    total_cents       bigint NOT NULL,
    currency          text NOT NULL DEFAULT 'USD',
    status            text NOT NULL DEFAULT 'pending',
    payment_status    text NOT NULL DEFAULT 'pending',
    payment_provider  text,
    payment_id        text,
    discount_id       uuid REFERENCES discounts(id) ON DELETE SET NULL,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at        timestamptz NOT NULL DEFAULT current_timestamp
);
CREATE INDEX idx_orders_customer ON orders(store_id, customer_email);
CREATE INDEX idx_orders_status ON orders(store_id, status, created_at DESC);

-- order_items
CREATE TABLE order_items (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id        uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id      uuid NOT NULL REFERENCES products(id),
    product_name    text NOT NULL,
    quantity        bigint NOT NULL,
    price_in_cents  bigint NOT NULL,
    total_cents     bigint NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT current_timestamp
);
CREATE INDEX idx_order_items_order ON order_items(order_id);

-- product_reviews
CREATE TABLE product_reviews (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id        uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id      uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    customer_name   text NOT NULL,
    customer_email  text NOT NULL,
    rating          bigint NOT NULL,                    -- 1-5
    title           text,
    body            text,
    is_verified     boolean NOT NULL DEFAULT false,     -- verified purchase
    status          text NOT NULL DEFAULT 'approved',   -- approved, pending, rejected
    created_at      timestamptz NOT NULL DEFAULT current_timestamp
);
CREATE INDEX idx_reviews_product ON product_reviews(product_id, status);

-- wishlists
CREATE TABLE wishlists (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id    uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id  uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    session_id  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (store_id, session_id, product_id)
);

COMMIT;
