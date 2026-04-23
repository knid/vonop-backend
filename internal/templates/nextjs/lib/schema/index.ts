// Drizzle schema mirroring internal/starters/ecommerce_schema.sql. If the SQL
// changes, update this file to match.

import {
  pgTable,
  uuid,
  text,
  bigint,
  boolean,
  doublePrecision,
  timestamp,
  jsonb,
  index,
  uniqueIndex,
  type AnyPgColumn,
} from "drizzle-orm/pg-core";
import { sql } from "drizzle-orm";

// config (exactly one row per tenant DB; UPDATE-only after seeding)
export const config = pgTable("config", {
  id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
  storeName: text("store_name").notNull().default("Your Store"),
  currency: text("currency").notNull().default("USD"),
  locale: text("locale").notNull().default("en"),
  taxRate: doublePrecision("tax_rate").notNull().default(0),
  shippingConfig: jsonb("shipping_config").notNull().default(sql`'{}'::jsonb`),
  paymentConfig: jsonb("payment_config").notNull().default(sql`'{}'::jsonb`),
  notifyEmail: text("notify_email"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
});

// product_categories
export const productCategories = pgTable(
  "product_categories",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    name: text("name").notNull(),
    slug: text("slug").notNull().unique(),
    parentId: uuid("parent_id").references(
      (): AnyPgColumn => productCategories.id,
      { onDelete: "set null" },
    ),
    image: text("image"),
    description: text("description"),
    sortOrder: bigint("sort_order", { mode: "number" }).notNull().default(0),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
    deletedAt: timestamp("deleted_at", { withTimezone: true }),
  },
  (t) => ({
    parentIdx: index("idx_product_categories_parent")
      .on(t.parentId)
      .where(sql`deleted_at IS NULL`),
  }),
);

// products
export const products = pgTable(
  "products",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    name: text("name").notNull(),
    slug: text("slug").notNull().unique(),
    description: text("description"),
    images: jsonb("images").notNull().default(sql`'[]'::jsonb`),
    categoryId: uuid("category_id").references(() => productCategories.id, {
      onDelete: "set null",
    }),
    tags: jsonb("tags").notNull().default(sql`'[]'::jsonb`),
    status: text("status").notNull().default("active"),
    priceInCents: bigint("price_in_cents", { mode: "number" }).notNull().default(0),
    compareAtPrice: bigint("compare_at_price", { mode: "number" }).default(0),
    sku: text("sku"),
    inventory: bigint("inventory", { mode: "number" }).notNull().default(0),
    trackInventory: boolean("track_inventory").notNull().default(false),
    metadata: jsonb("metadata").notNull().default(sql`'{}'::jsonb`),
    seo: jsonb("seo").notNull().default(sql`'{}'::jsonb`),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
    deletedAt: timestamp("deleted_at", { withTimezone: true }),
  },
  (t) => ({
    statusIdx: index("products_status_idx")
      .on(t.status)
      .where(sql`deleted_at IS NULL`),
    categoryIdx: index("idx_products_category")
      .on(t.categoryId)
      .where(sql`deleted_at IS NULL`),
  }),
);

// discounts
export const discounts = pgTable(
  "discounts",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    code: text("code").notNull().unique(),
    type: text("type").notNull(),
    value: bigint("value", { mode: "number" }).notNull(),
    minOrderCents: bigint("min_order_cents", { mode: "number" }).notNull().default(0),
    maxUses: bigint("max_uses", { mode: "number" }).notNull().default(0),
    usedCount: bigint("used_count", { mode: "number" }).notNull().default(0),
    appliesTo: jsonb("applies_to").notNull().default(sql`'{}'::jsonb`),
    startsAt: timestamp("starts_at", { withTimezone: true }),
    endsAt: timestamp("ends_at", { withTimezone: true }),
    isActive: boolean("is_active").notNull().default(true),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
    deletedAt: timestamp("deleted_at", { withTimezone: true }),
  },
  (t) => ({
    activeIdx: index("discounts_active_idx").on(t.isActive),
  }),
);

// carts
export const carts = pgTable("carts", {
  id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
  sessionId: text("session_id").notNull().unique(),
  customerEmail: text("customer_email"),
  discountId: uuid("discount_id").references(() => discounts.id, {
    onDelete: "set null",
  }),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
});

// cart_items
export const cartItems = pgTable(
  "cart_items",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    cartId: uuid("cart_id")
      .notNull()
      .references(() => carts.id, { onDelete: "cascade" }),
    productId: uuid("product_id")
      .notNull()
      .references(() => products.id),
    quantity: bigint("quantity", { mode: "number" }).notNull().default(1),
    priceAtAddCents: bigint("price_at_add_cents", { mode: "number" }).notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    cartIdx: index("idx_cart_items_cart").on(t.cartId),
  }),
);

// orders
export const orders = pgTable(
  "orders",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    orderNumber: text("order_number").notNull().unique(),
    customerEmail: text("customer_email").notNull(),
    customerName: text("customer_name").notNull(),
    shippingAddress: jsonb("shipping_address").notNull().default(sql`'{}'::jsonb`),
    billingAddress: jsonb("billing_address").notNull().default(sql`'{}'::jsonb`),
    subtotalCents: bigint("subtotal_cents", { mode: "number" }).notNull(),
    discountCents: bigint("discount_cents", { mode: "number" }).notNull().default(0),
    taxCents: bigint("tax_cents", { mode: "number" }).notNull().default(0),
    shippingCents: bigint("shipping_cents", { mode: "number" }).notNull().default(0),
    totalCents: bigint("total_cents", { mode: "number" }).notNull(),
    currency: text("currency").notNull().default("USD"),
    status: text("status").notNull().default("pending"),
    paymentStatus: text("payment_status").notNull().default("pending"),
    paymentProvider: text("payment_provider"),
    paymentId: text("payment_id"),
    discountId: uuid("discount_id").references(() => discounts.id, {
      onDelete: "set null",
    }),
    metadata: jsonb("metadata").notNull().default(sql`'{}'::jsonb`),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    customerIdx: index("idx_orders_customer").on(t.customerEmail),
    statusIdx: index("idx_orders_status").on(t.status, t.createdAt.desc()),
  }),
);

// order_items
export const orderItems = pgTable(
  "order_items",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    orderId: uuid("order_id")
      .notNull()
      .references(() => orders.id, { onDelete: "cascade" }),
    productId: uuid("product_id")
      .notNull()
      .references(() => products.id),
    productName: text("product_name").notNull(),
    quantity: bigint("quantity", { mode: "number" }).notNull(),
    priceInCents: bigint("price_in_cents", { mode: "number" }).notNull(),
    totalCents: bigint("total_cents", { mode: "number" }).notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    orderIdx: index("idx_order_items_order").on(t.orderId),
  }),
);

// product_reviews
export const productReviews = pgTable(
  "product_reviews",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    productId: uuid("product_id")
      .notNull()
      .references(() => products.id, { onDelete: "cascade" }),
    customerName: text("customer_name").notNull(),
    customerEmail: text("customer_email").notNull(),
    rating: bigint("rating", { mode: "number" }).notNull(),
    title: text("title"),
    body: text("body"),
    isVerified: boolean("is_verified").notNull().default(false),
    status: text("status").notNull().default("approved"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    productIdx: index("idx_product_reviews_product").on(t.productId, t.status),
  }),
);

// wishlists
export const wishlists = pgTable(
  "wishlists",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    productId: uuid("product_id")
      .notNull()
      .references(() => products.id, { onDelete: "cascade" }),
    sessionId: text("session_id").notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    sessionProductUnique: uniqueIndex("wishlists_session_product_idx").on(
      t.sessionId,
      t.productId,
    ),
  }),
);

// contact_messages
export const contactMessages = pgTable(
  "contact_messages",
  {
    id: uuid("id").primaryKey().default(sql`uuid_generate_v4()`),
    name: text("name").notNull(),
    email: text("email").notNull(),
    subject: text("subject"),
    body: text("body").notNull(),
    status: text("status").notNull().default("new"),
    ipAddress: text("ip_address"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (t) => ({
    statusIdx: index("idx_contact_messages_status").on(t.status, t.createdAt.desc()),
  }),
);

// Type aliases via $inferSelect
export type Config = typeof config.$inferSelect;
export type ProductCategory = typeof productCategories.$inferSelect;
export type Product = typeof products.$inferSelect;
export type Discount = typeof discounts.$inferSelect;
export type Cart = typeof carts.$inferSelect;
export type CartItem = typeof cartItems.$inferSelect;
export type Order = typeof orders.$inferSelect;
export type OrderItem = typeof orderItems.$inferSelect;
export type ProductReview = typeof productReviews.$inferSelect;
export type Wishlist = typeof wishlists.$inferSelect;
export type ContactMessage = typeof contactMessages.$inferSelect;
