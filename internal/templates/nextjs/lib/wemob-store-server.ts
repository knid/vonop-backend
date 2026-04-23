import "server-only";
// lib/wemob-store-server.ts — Drizzle-backed storefront read functions.
// This file is template-managed. Do not modify.
//
// All exports here run on the server (Node runtime) and talk directly to the
// tenant's Neon database via drizzle. Import these from server components and
// route handlers only — never from a client component. The `server-only`
// import above causes `next build` to fail fast if that happens.
//
// Client-side write flows (cart mutations, checkout, review submission, etc.)
// still live in `./wemob-store.ts` and currently call the legacy
// `/v2/storefront/*` API. They will be migrated to local route handlers in a
// follow-up PR.

import { cache } from "react";

import { and, asc, desc, eq, ilike, inArray, isNull, sql } from "drizzle-orm";

import { db } from "./db";
import {
  cartItems,
  carts,
  discounts,
  orderItems,
  orders,
  productCategories,
  productReviews,
  products,
  stores,
  wishlists,
  type Store,
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
// Store helper
// ---------------------------------------------------------------------------

// Each tenant DB holds exactly one `stores` row (not soft-deleted). React's
// `cache()` scopes memoization to a single render pass, so repeated calls
// within one request share a result without leaking across requests on a
// persistent Node runtime.
const getStore = cache(async (): Promise<Store> => {
  const rows = await db
    .select()
    .from(stores)
    .where(isNull(stores.deletedAt))
    .limit(1);
  if (rows.length === 0) {
    throw new Error("Store row not found in tenant database");
  }
  return rows[0];
});

// ---------------------------------------------------------------------------
// Store config
// ---------------------------------------------------------------------------

/** Fetch the store configuration derived from the single `stores` row. */
export async function getStoreConfig(): Promise<StoreConfig> {
  const store = await getStore();

  // Derive payment_providers from stores.payment_config jsonb. Expected shape
  // is an object keyed by provider name, with `{ enabled: boolean }` values.
  // Fallback: emit every non-empty top-level key. The legacy Go API derived
  // this from AppIntegration rows in the main platform DB, which this tenant
  // DB has no visibility into — this is a documented divergence.
  const paymentProviders: string[] = [];
  const paymentConfig =
    (store.paymentConfig as Record<string, unknown> | null) ?? {};
  for (const [provider, cfg] of Object.entries(paymentConfig)) {
    if (cfg && typeof cfg === "object") {
      const enabled = (cfg as Record<string, unknown>).enabled;
      if (enabled === true) {
        paymentProviders.push(provider);
        continue;
      }
      // No explicit `enabled` key — treat presence as opt-in.
      if (!("enabled" in (cfg as Record<string, unknown>))) {
        paymentProviders.push(provider);
      }
    }
  }

  // Shipping enabled: mirror the Go logic — truthy if shipping_config has any
  // keys, overridden by an explicit `enabled` flag when present.
  const shippingConfig =
    (store.shippingConfig as Record<string, unknown> | null) ?? {};
  let shippingEnabled = Object.keys(shippingConfig).length > 0;
  if (typeof shippingConfig.enabled === "boolean") {
    shippingEnabled = shippingConfig.enabled;
  }

  return {
    name: store.storeName,
    currency: store.currency,
    locale: store.locale,
    tax_rate: store.taxRate,
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
  const store = await getStore();

  const whereConditions = [
    eq(products.storeId, store.id),
    eq(products.status, "active"),
    isNull(products.deletedAt),
  ];

  if (params?.category) {
    // Resolve category slug -> category id scoped to this store.
    const catRows = await db
      .select({ id: productCategories.id })
      .from(productCategories)
      .where(
        and(
          eq(productCategories.storeId, store.id),
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
  const store = await getStore();

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
        eq(products.storeId, store.id),
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
  const store = await getStore();

  const rows = await db
    .select()
    .from(productCategories)
    .where(
      and(
        eq(productCategories.storeId, store.id),
        isNull(productCategories.deletedAt),
      ),
    )
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
  const store = await getStore();

  const cartRows = await db
    .select()
    .from(carts)
    .where(and(eq(carts.storeId, store.id), eq(carts.sessionId, sessionId)))
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

  // Resolve discount (if any), mirroring the server-side buildCartResponse
  // logic: percentage/fixed types, MinOrderCents guard, subtotal cap.
  let discountCents = 0;
  let discountCode = "";
  if (cart.discountId) {
    const discountRows = await db
      .select()
      .from(discounts)
      .where(
        and(eq(discounts.id, cart.discountId), isNull(discounts.deletedAt)),
      )
      .limit(1);
    if (discountRows.length > 0) {
      const discount = discountRows[0];
      if (discount.isActive) {
        discountCode = discount.code;
        if (discount.type === "percentage") {
          discountCents = Math.floor((subtotalCents * discount.value) / 100);
        } else {
          discountCents = discount.value;
        }
        if (discount.minOrderCents > 0 && subtotalCents < discount.minOrderCents) {
          discountCents = 0;
        }
        if (discountCents > subtotalCents) {
          discountCents = subtotalCents;
        }
      }
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

/** Fetch an order by id, scoped to the single store, with its line items. */
export async function getOrder(orderId: string): Promise<OrderTracking | null> {
  const store = await getStore();

  const orderRows = await db
    .select()
    .from(orders)
    .where(and(eq(orders.storeId, store.id), eq(orders.id, orderId)))
    .limit(1);

  if (orderRows.length === 0) {
    return null;
  }
  const order = orderRows[0];

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
  const store = await getStore();

  const productRows = await db
    .select({ id: products.id })
    .from(products)
    .where(
      and(
        eq(products.storeId, store.id),
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

/** Fetch wishlist rows for a session, scoped to the single store. */
export async function getWishlist(sessionId: string): Promise<WishlistItem[]> {
  const store = await getStore();

  const rows = await db
    .select()
    .from(wishlists)
    .where(
      and(
        eq(wishlists.storeId, store.id),
        eq(wishlists.sessionId, sessionId),
      ),
    )
    .orderBy(desc(wishlists.createdAt));

  return rows.map((w) => ({
    id: w.id,
    product_id: w.productId,
    created_at: w.createdAt.toISOString(),
  }));
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
