import "server-only";
// lib/wemob-store-server.ts — Drizzle-backed storefront read functions.
// This file is template-managed. Do not modify.
//
// All exports here run on the server (Node runtime) and talk directly to the
// tenant's Neon database via drizzle. Import these from server components and
// route handlers only — never from a client component. The `server-only`
// import above causes `next build` to fail fast if that happens.
//
// The tenant DB is already scoped to one store — there is no store_id column
// anywhere in the schema. Store-wide settings live on the single-row `config`
// table, which the AI UPDATEs (never INSERTs).
//
// Client-side write flows (cart mutations, checkout, review submission, etc.)
// still live in `./wemob-store.ts` and currently call the legacy
// `/v2/storefront/*` API. They will be migrated to local route handlers in a
// follow-up PR.

import { and, asc, desc, eq, ilike, inArray, isNull, sql } from "drizzle-orm";

import { db } from "./db";
import { resolveDiscountById } from "./discounts";
import {
  cartItems,
  carts,
  config,
  orderItems,
  orders,
  productCategories,
  productReviews,
  products,
  wishlists,
} from "./schema";
import type {
  OrderTracking,
  OrderTrackingItem,
  ProductReview,
  ReviewsResult,
  StoreConfig,
  StorefrontCart,
  StorefrontCartItem,
  StorefrontCategory,
  StorefrontProduct,
  WishlistItem,
} from "./wemob-store-types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Escape Postgres LIKE/ILIKE wildcards (`%`, `_`, `\`) so user-supplied search
// input is treated as literal text rather than silently broadening the match.
function escapeLike(input: string): string {
  return input.replace(/[\\%_]/g, (m) => `\\${m}`);
}

// ---------------------------------------------------------------------------
// Store config
// ---------------------------------------------------------------------------

/** Fetch the store configuration from the single `config` row. */
export async function getStoreConfig(): Promise<StoreConfig> {
  const rows = await db.select().from(config).limit(1);
  if (rows.length === 0) {
    throw new Error("Config row not found in tenant database");
  }
  const cfg = rows[0];

  // Derive payment_providers from config.payment_config jsonb. Expected shape
  // is an object keyed by provider name, with `{ enabled: boolean }` values.
  // Fallback: emit every non-empty top-level key. The legacy Go API derived
  // this from AppIntegration rows in the main platform DB, which this tenant
  // DB has no visibility into — this is a documented divergence.
  const paymentProviders: string[] = [];
  const paymentConfig =
    (cfg.paymentConfig as Record<string, unknown> | null) ?? {};
  for (const [provider, providerCfg] of Object.entries(paymentConfig)) {
    if (providerCfg && typeof providerCfg === "object") {
      const enabled = (providerCfg as Record<string, unknown>).enabled;
      if (enabled === true) {
        paymentProviders.push(provider);
        continue;
      }
      // No explicit `enabled` key — treat presence as opt-in.
      if (!("enabled" in (providerCfg as Record<string, unknown>))) {
        paymentProviders.push(provider);
      }
    }
  }

  if (cfg.currency === "TRY") {
    paymentProviders.sort((a, b) => {
      if (a === b) return 0;
      if (a === "iyzico") return -1;
      if (b === "iyzico") return 1;
      if (a === "stripe") return -1;
      if (b === "stripe") return 1;
      return 0;
    });
  }

  // Shipping enabled: mirror the Go logic — truthy if shipping_config has any
  // keys, overridden by an explicit `enabled` flag when present.
  const shippingConfig =
    (cfg.shippingConfig as Record<string, unknown> | null) ?? {};
  let shippingEnabled = Object.keys(shippingConfig).length > 0;
  if (typeof shippingConfig.enabled === "boolean") {
    shippingEnabled = shippingConfig.enabled;
  }

  return {
    name: cfg.storeName,
    currency: cfg.currency,
    locale: cfg.locale,
    tax_rate: cfg.taxRate,
    payment_providers: paymentProviders,
    shipping_enabled: shippingEnabled,
  };
}

// ---------------------------------------------------------------------------
// Products
// ---------------------------------------------------------------------------

interface GetProductsParams {
  category?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

/**
 * Fetch active, non-deleted products. Optionally filter by category slug or
 * a case-insensitive search query matched against product name.
 */
export async function getProducts(
  params?: GetProductsParams,
): Promise<StorefrontProduct[]> {
  const whereConditions = [
    eq(products.status, "active"),
    isNull(products.deletedAt),
  ];

  if (params?.category) {
    // Resolve category slug -> category id.
    const catRows = await db
      .select({ id: productCategories.id })
      .from(productCategories)
      .where(
        and(
          eq(productCategories.slug, params.category),
          isNull(productCategories.deletedAt),
        ),
      )
      .limit(1);
    if (catRows.length === 0) {
      return [];
    }
    whereConditions.push(eq(products.categoryId, catRows[0].id));
  }

  if (params?.q) {
    whereConditions.push(ilike(products.name, `%${escapeLike(params.q)}%`));
  }

  let query = db
    .select({
      product: products,
      categoryName: productCategories.name,
      categorySlug: productCategories.slug,
    })
    .from(products)
    .leftJoin(productCategories, eq(products.categoryId, productCategories.id))
    .where(and(...whereConditions))
    .orderBy(desc(products.createdAt))
    .$dynamic();

  if (typeof params?.limit === "number" && params.limit > 0) {
    query = query.limit(params.limit);
  }
  if (typeof params?.offset === "number" && params.offset > 0) {
    query = query.offset(params.offset);
  }

  const rows = await query;
  if (rows.length === 0) {
    return [];
  }

  // Gather review aggregates for all product IDs in a single query.
  const productIds = rows.map((r) => r.product.id);
  const reviewAgg = await getReviewAggregates(productIds);

  return rows.map((row) =>
    toStorefrontProduct(
      row.product,
      row.categoryName,
      row.categorySlug,
      reviewAgg.get(row.product.id),
    ),
  );
}

/** Fetch a single active, non-deleted product by slug. */
export async function getProduct(
  slug: string,
): Promise<StorefrontProduct | null> {
  const rows = await db
    .select({
      product: products,
      categoryName: productCategories.name,
      categorySlug: productCategories.slug,
    })
    .from(products)
    .leftJoin(productCategories, eq(products.categoryId, productCategories.id))
    .where(
      and(
        eq(products.slug, slug),
        eq(products.status, "active"),
        isNull(products.deletedAt),
      ),
    )
    .limit(1);

  if (rows.length === 0) {
    return null;
  }

  const row = rows[0];
  const reviewAgg = await getReviewAggregates([row.product.id]);

  return toStorefrontProduct(
    row.product,
    row.categoryName,
    row.categorySlug,
    reviewAgg.get(row.product.id),
  );
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

/** Fetch all non-deleted categories ordered by sort_order. */
export async function getCategories(): Promise<StorefrontCategory[]> {
  const rows = await db
    .select()
    .from(productCategories)
    .where(isNull(productCategories.deletedAt))
    .orderBy(asc(productCategories.sortOrder), asc(productCategories.name));

  return rows.map((c) => ({
    id: c.id,
    name: c.name,
    slug: c.slug,
    image: c.image ?? "",
    description: c.description ?? "",
  }));
}

// ---------------------------------------------------------------------------
// Cart
// ---------------------------------------------------------------------------

/**
 * Fetch the cart for a session, including items joined to their product for
 * display name/image, plus discount code + computed totals. Returns null
 * when no cart row exists for the session.
 */
export async function getCart(sessionId: string): Promise<StorefrontCart | null> {
  const cartRows = await db
    .select()
    .from(carts)
    .where(eq(carts.sessionId, sessionId))
    .limit(1);

  if (cartRows.length === 0) {
    return null;
  }
  const cart = cartRows[0];

  const itemRows = await db
    .select({
      item: cartItems,
      productName: products.name,
      productImages: products.images,
    })
    .from(cartItems)
    .leftJoin(products, eq(cartItems.productId, products.id))
    .where(eq(cartItems.cartId, cart.id));

  let subtotalCents = 0;
  const items: StorefrontCartItem[] = itemRows.map((row) => {
    const total = row.item.priceAtAddCents * row.item.quantity;
    subtotalCents += total;
    const imgs = (row.productImages as string[] | null) ?? [];
    return {
      id: row.item.id,
      product_id: row.item.productId,
      product_name: row.productName ?? "",
      product_image: imgs.length > 0 ? imgs[0] : "",
      quantity: row.item.quantity,
      price_cents: row.item.priceAtAddCents,
      total_cents: total,
    };
  });

  // Resolve discount through the shared helper so cart view, apply endpoint,
  // and checkout all apply the same validity gates (expiry, max_uses,
  // min_order). The cart drawer displays empty/zero when the row was valid
  // at apply-time but has since expired or been exhausted. When the discount
  // becomes permanently invalid (not_found / expired / exhausted) we also
  // null out the cart's discount_id so downstream reads stop paying the
  // resolve cost and don't surface a "remove discount" button for nothing.
  // We skip this for `below_minimum` because the customer can still reach
  // the threshold by adding items.
  let discountCents = 0;
  let discountCode = "";
  if (cart.discountId) {
    const resolution = await resolveDiscountById(cart.discountId, subtotalCents);
    if (resolution.kind === "applied") {
      discountCode = resolution.discount.code;
      discountCents = resolution.cents;
    } else if (resolution.reason !== "below_minimum") {
      await db
        .update(carts)
        .set({ discountId: null, updatedAt: new Date() })
        .where(eq(carts.id, cart.id));
    }
  }

  return {
    id: cart.id,
    session_id: cart.sessionId,
    items,
    subtotal_cents: subtotalCents,
    discount_code: discountCode,
    discount_cents: discountCents,
    total_cents: subtotalCents - discountCents,
  };
}

// ---------------------------------------------------------------------------
// Orders
// ---------------------------------------------------------------------------

/** Fetch an order by id, with its line items. The viewToken is the IDOR gate
 * set at checkout init and threaded through the success/cancel redirect URLs —
 * we return null when it's missing or doesn't match so a scraper with just
 * the order UUID can't read customer PII. */
export async function getOrder(
  orderId: string,
  viewToken: string,
): Promise<OrderTracking | null> {
  const orderRows = await db
    .select()
    .from(orders)
    .where(eq(orders.id, orderId))
    .limit(1);

  if (orderRows.length === 0) {
    return null;
  }
  const order = orderRows[0];
  const expected =
    (order.metadata as Record<string, unknown> | null)?.viewToken;
  // Orders written before the viewToken gate shipped have no `viewToken` on
  // metadata. Those rows stay viewable by id alone — otherwise the moment
  // the updated template redeploys, every in-flight and historical order
  // confirmation link silently 404s. New orders set the token on insert,
  // so anything with `expected` defined must match exactly.
  if (expected !== undefined) {
    if (typeof expected !== "string" || !expected || expected !== viewToken) {
      return null;
    }
  }

  const itemRows = await db
    .select()
    .from(orderItems)
    .where(eq(orderItems.orderId, order.id));

  const items: OrderTrackingItem[] = itemRows.map((i) => ({
    product_name: i.productName,
    quantity: i.quantity,
    price_in_cents: i.priceInCents,
    total_cents: i.totalCents,
  }));

  return {
    order_id: order.id,
    order_number: order.orderNumber,
    status: order.status,
    payment_status: order.paymentStatus,
    items,
    subtotal_cents: order.subtotalCents,
    discount_cents: order.discountCents,
    tax_cents: order.taxCents,
    shipping_cents: order.shippingCents,
    total_cents: order.totalCents,
    currency: order.currency,
    created_at: order.createdAt.toISOString(),
  };
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------

/**
 * Fetch approved reviews for a product by slug, plus aggregate stats. Returns
 * empty/zero values when the product is missing — matching the legacy API's
 * shape so pages render without branching.
 */
export async function getProductReviews(slug: string): Promise<ReviewsResult> {
  const productRows = await db
    .select({ id: products.id })
    .from(products)
    .where(
      and(
        eq(products.slug, slug),
        isNull(products.deletedAt),
      ),
    )
    .limit(1);

  if (productRows.length === 0) {
    return { reviews: [], average_rating: 0, review_count: 0 };
  }

  const productId = productRows[0].id;

  const reviewRows = await db
    .select()
    .from(productReviews)
    .where(
      and(
        eq(productReviews.productId, productId),
        eq(productReviews.status, "approved"),
      ),
    )
    .orderBy(desc(productReviews.createdAt));

  const reviews: ProductReview[] = reviewRows.map((r) => ({
    id: r.id,
    customer_name: r.customerName,
    rating: r.rating,
    title: r.title ?? "",
    body: r.body ?? "",
    is_verified: r.isVerified,
    created_at: r.createdAt.toISOString(),
  }));

  const count = reviews.length;
  const average =
    count === 0
      ? 0
      : reviews.reduce((sum, r) => sum + r.rating, 0) / count;

  return { reviews, average_rating: average, review_count: count };
}

// ---------------------------------------------------------------------------
// Wishlist
// ---------------------------------------------------------------------------

/** Fetch wishlist rows joined to product data for a session. Archived and
 * soft-deleted products are filtered out. */
export async function getWishlist(sessionId: string): Promise<WishlistItem[]> {
  const rows = await db
    .select({
      id: wishlists.id,
      productId: wishlists.productId,
      createdAt: wishlists.createdAt,
      productName: products.name,
      productSlug: products.slug,
      productImages: products.images,
      productPriceInCents: products.priceInCents,
      productInventory: products.inventory,
      productTrackInventory: products.trackInventory,
    })
    .from(wishlists)
    .innerJoin(
      products,
      and(
        eq(wishlists.productId, products.id),
        eq(products.status, "active"),
        isNull(products.deletedAt),
      ),
    )
    .where(eq(wishlists.sessionId, sessionId))
    .orderBy(desc(wishlists.createdAt));

  return rows.map((w) => {
    const imgs = (w.productImages as string[] | null) ?? [];
    return {
      id: w.id,
      product_id: w.productId,
      created_at: w.createdAt.toISOString(),
      product_name: w.productName,
      product_slug: w.productSlug,
      product_image: imgs.length > 0 ? imgs[0] : "",
      price_cents: w.productPriceInCents,
      in_stock: !w.productTrackInventory || w.productInventory > 0,
    };
  });
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/**
 * Fetch review aggregates (count + average) for a batch of product ids in a
 * single query. Returns a Map keyed by product id.
 */
async function getReviewAggregates(
  productIds: string[],
): Promise<Map<string, { count: number; average: number }>> {
  const result = new Map<string, { count: number; average: number }>();
  if (productIds.length === 0) return result;

  const rows = await db
    .select({
      productId: productReviews.productId,
      count: sql<number>`count(*)::int`.as("count"),
      average: sql<number>`coalesce(avg(${productReviews.rating}), 0)::float8`.as(
        "average",
      ),
    })
    .from(productReviews)
    .where(
      and(
        inArray(productReviews.productId, productIds),
        eq(productReviews.status, "approved"),
      ),
    )
    .groupBy(productReviews.productId);

  for (const row of rows) {
    result.set(row.productId, {
      count: Number(row.count) || 0,
      average: Number(row.average) || 0,
    });
  }
  return result;
}

/** Build a StorefrontProduct response from a product row + joined category. */
function toStorefrontProduct(
  p: typeof products.$inferSelect,
  categoryName: string | null,
  categorySlug: string | null,
  agg: { count: number; average: number } | undefined,
): StorefrontProduct {
  const images = (p.images as string[] | null) ?? [];
  const tags = (p.tags as string[] | null) ?? [];
  const compareAt = p.compareAtPrice ?? 0;
  const inStock = !p.trackInventory || p.inventory > 0;

  return {
    id: p.id,
    name: p.name,
    slug: p.slug,
    description: p.description ?? "",
    images,
    category: categoryName ?? "",
    category_slug: categorySlug ?? "",
    tags,
    price_in_cents: p.priceInCents,
    compare_at_price: compareAt,
    // Without product variants there's no real min/max range — mirror the
    // legacy API and repeat the base price in both fields.
    min_price_cents: p.priceInCents,
    max_price_cents: p.priceInCents,
    in_stock: inStock,
    average_rating: agg?.average ?? 0,
    review_count: agg?.count ?? 0,
  };
}
