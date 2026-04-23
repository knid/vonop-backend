// lib/wemob-store-types.ts — Shared storefront type declarations.
//
// These interfaces are imported by both client-side (`wemob-store.ts`) and
// server-side (`wemob-store-server.ts`) storefront modules. This file itself
// contains no runtime code (no hooks, no DB access) so it is safe to import
// from either a client or server component.

// ---------------------------------------------------------------------------
// Store config
// ---------------------------------------------------------------------------

export interface StoreConfig {
  name: string;
  currency: string;
  locale: string;
  tax_rate: number;
  payment_providers: string[];
  shipping_enabled: boolean;
}

// ---------------------------------------------------------------------------
// Products & categories
// ---------------------------------------------------------------------------

export interface StorefrontProduct {
  id: string;
  name: string;
  slug: string;
  description: string;
  images: string[];
  category: string;
  category_slug: string;
  tags: string[];
  price_in_cents: number;
  compare_at_price: number;
  min_price_cents: number;
  max_price_cents: number;
  in_stock: boolean;
  average_rating: number;
  review_count: number;
}

export interface StorefrontCategory {
  id: string;
  name: string;
  slug: string;
  image: string;
  description: string;
}

// ---------------------------------------------------------------------------
// Cart
// ---------------------------------------------------------------------------

export interface StorefrontCart {
  id: string;
  session_id: string;
  items: StorefrontCartItem[];
  subtotal_cents: number;
  discount_code: string;
  discount_cents: number;
  total_cents: number;
}

export interface StorefrontCartItem {
  id: string;
  product_id: string;
  product_name: string;
  product_image: string;
  quantity: number;
  price_cents: number;
  total_cents: number;
}

// ---------------------------------------------------------------------------
// Orders
// ---------------------------------------------------------------------------

export interface OrderTracking {
  order_id: string;
  order_number: string;
  status: string;
  payment_status: string;
  items: OrderTrackingItem[];
  subtotal_cents: number;
  discount_cents: number;
  tax_cents: number;
  total_cents: number;
  currency: string;
  created_at: string;
}

export interface OrderTrackingItem {
  product_name: string;
  quantity: number;
  price_in_cents: number;
  total_cents: number;
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------

export interface ProductReview {
  id: string;
  customer_name: string;
  rating: number;
  title: string;
  body: string;
  is_verified: boolean;
  created_at: string;
}

export interface ReviewsResult {
  reviews: ProductReview[];
  average_rating: number;
  review_count: number;
}

// ---------------------------------------------------------------------------
// Wishlist
// ---------------------------------------------------------------------------

export interface WishlistItem {
  id: string;
  product_id: string;
  created_at: string;
  // Product fields are joined server-side so a wishlist page renders in a
  // single round-trip. Older clients that only read `product_id` keep working.
  product_name?: string;
  product_slug?: string;
  product_image?: string;
  price_cents?: number;
  in_stock?: boolean;
}
