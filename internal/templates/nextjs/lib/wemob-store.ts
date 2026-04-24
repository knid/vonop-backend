"use client";
// lib/wemob-store.ts — Wemob Storefront SDK (client-only).
// This file is template-managed. Do not modify.
//
// Design:
//   • Catalog reads (products, categories, config) are NOT served from this
//     file. Page-level Server Components import `./wemob-store-server`
//     directly and either render from SSR data or pass it down as props.
//     Store-wide config + categories are fetched once in app/layout.tsx
//     (server) and seeded into <StoreProvider> so descendants can read them
//     synchronously via useStoreConfig() / useCategories() without any
//     client-side fetch.
//   • Session-scoped reads (cart, wishlist) and every mutation go through
//     dedicated POST /api/* route handlers. The browser carries the
//     httpOnly wemob_cart_session cookie transparently.
//   • No Server Actions: Origin/X-Forwarded-Host CSRF checks under a reverse
//     proxy make them fragile, and catalog reads should be cacheable GETs
//     anyway.

import React from "react";
import type {
  StoreConfig,
  StorefrontProduct,
  StorefrontCategory,
  StorefrontCart,
  StorefrontCartItem,
  OrderTracking,
  OrderTrackingItem,
  ProductReview,
  ReviewsResult,
  WishlistItem,
} from "./wemob-store-types";

// Re-export shared types so downstream code can continue to
// `import { StorefrontProduct } from "@/lib/wemob-store"`.
export type {
  StoreConfig,
  StorefrontProduct,
  StorefrontCategory,
  StorefrontCart,
  StorefrontCartItem,
  OrderTracking,
  OrderTrackingItem,
  ProductReview,
  ReviewsResult,
  WishlistItem,
};

// ---------------------------------------------------------------------------
// Fetch helper for mutation endpoints
// ---------------------------------------------------------------------------

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...options?.headers },
    ...options,
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error("API error " + res.status + ": " + body);
  }
  return res.json();
}

// ---------------------------------------------------------------------------
// Store context (config + categories)
// ---------------------------------------------------------------------------
//
// Seeded from app/layout.tsx (Server Component) on every request. Child
// components read synchronously — there is no loading state because the
// data is already in the initial HTML payload.

interface StoreContextValue {
  config: StoreConfig;
  categories: StorefrontCategory[];
}

const StoreContext = React.createContext<StoreContextValue | null>(null);

// formatPrice() below is called from components that aren't wrapped in a
// hook, so we also mirror the currency + locale into a module-level cache
// on every StoreProvider render. `_configCache` is only read synchronously
// and is only written to the value coming from SSR, so hydration and
// subsequent navigations keep it coherent.
let _configCache: StoreConfig | null = null;

export function StoreProvider({
  config,
  categories,
  children,
}: {
  config: StoreConfig;
  categories: StorefrontCategory[];
  children: React.ReactNode;
}) {
  _configCache = config;
  const value = React.useMemo<StoreContextValue>(
    () => ({ config, categories }),
    [config, categories],
  );
  return React.createElement(StoreContext.Provider, { value }, children);
}

function useStoreContext(): StoreContextValue {
  const ctx = React.useContext(StoreContext);
  if (!ctx) {
    throw new Error(
      "useStoreConfig / useCategories must be used within a <StoreProvider>",
    );
  }
  return ctx;
}

/** Access the current store configuration. Always populated from SSR. */
export function useStoreConfig(): {
  config: StoreConfig;
  isLoading: false;
  error: null;
} {
  const { config } = useStoreContext();
  return { config, isLoading: false, error: null };
}

/** Access the full category list. Always populated from SSR. */
export function useCategories(): {
  categories: StorefrontCategory[];
  isLoading: false;
  error: null;
} {
  const { categories } = useStoreContext();
  return { categories, isLoading: false, error: null };
}

// ---------------------------------------------------------------------------
// Price formatting
// ---------------------------------------------------------------------------

/**
 * Format a price in cents to a human-readable currency string.
 * Uses the store config currency/locale from the nearest StoreProvider when
 * no currency is passed. Safe to call from any client component under the
 * provider; falls back to USD/en-US before the provider mounts (only
 * relevant during an error boundary render).
 */
export function formatPrice(cents: number, currency?: string): string {
  const cur = currency || _configCache?.currency || "USD";
  const locale = _configCache?.locale || "en-US";
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency: cur,
  }).format(cents / 100);
}

// ---------------------------------------------------------------------------
// Cart API
//
// Cart mutations target local Next.js route handlers that talk directly
// to the tenant's Neon DB. The browser automatically sends the httpOnly
// wemob_cart_session cookie owned by those routes — that is the SINGLE
// source of truth for which cart belongs to this browser.
// ---------------------------------------------------------------------------

function emptyCart(): StorefrontCart {
  return {
    id: "",
    session_id: "",
    items: [],
    subtotal_cents: 0,
    discount_code: "",
    discount_cents: 0,
    total_cents: 0,
  };
}

async function cartFetch(
  path: string,
  options?: RequestInit,
): Promise<StorefrontCart> {
  const data = await apiFetch<StorefrontCart | null>(path, options);
  return data ?? emptyCart();
}

export async function getCart(): Promise<StorefrontCart> {
  return cartFetch("/api/cart");
}

export async function addCartItem(
  productId: string,
  quantity: number,
): Promise<StorefrontCart> {
  return cartFetch("/api/cart", {
    method: "POST",
    body: JSON.stringify({ productId, quantity }),
  });
}

export async function updateCartItem(
  itemId: string,
  quantity: number,
): Promise<StorefrontCart> {
  return cartFetch("/api/cart/" + encodeURIComponent(itemId), {
    method: "PUT",
    body: JSON.stringify({ quantity }),
  });
}

export async function removeCartItem(itemId: string): Promise<StorefrontCart> {
  return cartFetch("/api/cart/" + encodeURIComponent(itemId), {
    method: "DELETE",
  });
}

export async function applyDiscountCode(code: string): Promise<StorefrontCart> {
  return cartFetch("/api/discount", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

/** Remove the currently applied discount code from the cart. */
export async function removeDiscount(): Promise<StorefrontCart> {
  return cartFetch("/api/discount", {
    method: "DELETE",
  });
}

// ---------------------------------------------------------------------------
// Internal: recalculate cart totals from items
// ---------------------------------------------------------------------------

function recalcTotals(c: StorefrontCart): StorefrontCart {
  const subtotal_cents = c.items.reduce(
    (sum, i) => sum + i.price_cents * i.quantity,
    0,
  );
  return {
    ...c,
    subtotal_cents,
    total_cents: subtotal_cents - c.discount_cents,
  };
}

// ---------------------------------------------------------------------------
// Cart Context
// ---------------------------------------------------------------------------

// Optional product hint passed into addItem so the optimistic drawer row and
// badge render with the real name/image/price instead of flashing $0 for the
// round-trip. Callers that already have the product (product detail page,
// product card) pass it; callers without it (deep-linked add flows) can omit.
export interface AddItemProductHint {
  name: string;
  image: string;
  priceInCents: number;
}

interface CartContextValue {
  cart: StorefrontCart | null;
  isLoading: boolean;
  itemCount: number;
  addItem: (
    productId: string,
    quantity?: number,
    hint?: AddItemProductHint,
  ) => Promise<void>;
  updateItem: (itemId: string, quantity: number) => Promise<void>;
  removeItem: (itemId: string) => Promise<void>;
  applyDiscount: (code: string) => Promise<void>;
  removeDiscountCode: () => Promise<void>;
  clearCart: () => void;
}

const CartContext = React.createContext<CartContextValue | null>(null);

// ---------------------------------------------------------------------------
// CartProvider
// ---------------------------------------------------------------------------

export function CartProvider({ children }: { children: React.ReactNode }) {
  const [cart, setCart] = React.useState<StorefrontCart | null>(null);
  const [isLoading, setIsLoading] = React.useState(true);
  const cartRef = React.useRef<StorefrontCart | null>(null);

  React.useEffect(() => {
    cartRef.current = cart;
  }, [cart]);

  const itemCount = React.useMemo(() => {
    if (!cart || !cart.items) return 0;
    return cart.items.reduce((sum, item) => sum + item.quantity, 0);
  }, [cart]);

  // Initialize cart on mount. No client-side session bootstrap — the httpOnly
  // wemob_cart_session cookie is minted by the first /api/cart mutation and
  // the browser carries it transparently after that.
  React.useEffect(() => {
    let cancelled = false;

    async function init() {
      try {
        const existing = await getCart();
        if (!cancelled) setCart(existing);
      } catch {
        // API unreachable: leave cart as null. Carts are server-created
        // lazily on the first add-item so nothing further is needed here.
      }
      if (!cancelled) setIsLoading(false);
    }

    init();
    return () => {
      cancelled = true;
    };
  }, []);

  const withOptimistic = React.useCallback(
    async (
      optimisticCart: StorefrontCart | null,
      apiCall: () => Promise<StorefrontCart>,
    ) => {
      const previous = cartRef.current;
      if (optimisticCart) setCart(optimisticCart);

      try {
        const updated = await apiCall();
        setCart(updated);
      } catch (err) {
        setCart(previous);
        throw err;
      }
    },
    [],
  );

  const handleAddItem = React.useCallback(
    async (productId: string, quantity?: number, hint?: AddItemProductHint) => {
      const qty = Math.max(1, quantity ?? 1);

      const current = cartRef.current;
      const optimistic = current
        ? recalcTotals({
            ...current,
            items: [
              ...current.items,
              {
                id: "__optimistic__" + Date.now(),
                product_id: productId,
                product_name: hint?.name ?? "",
                product_image: hint?.image ?? "",
                quantity: qty,
                price_cents: hint?.priceInCents ?? 0,
                total_cents: (hint?.priceInCents ?? 0) * qty,
              },
            ],
          })
        : null;

      await withOptimistic(optimistic, () => addCartItem(productId, qty));
    },
    [withOptimistic],
  );

  const handleUpdateItem = React.useCallback(
    async (itemId: string, quantity: number) => {
      const current = cartRef.current;
      if (!current) return;

      const optimistic = recalcTotals({
        ...current,
        items: current.items.map((item) =>
          item.id === itemId
            ? { ...item, quantity, total_cents: item.price_cents * quantity }
            : item,
        ),
      });

      await withOptimistic(optimistic, () => updateCartItem(itemId, quantity));
    },
    [withOptimistic],
  );

  const handleRemoveItem = React.useCallback(
    async (itemId: string) => {
      const current = cartRef.current;
      if (!current) return;

      const optimistic = recalcTotals({
        ...current,
        items: current.items.filter((item) => item.id !== itemId),
      });

      await withOptimistic(optimistic, () => removeCartItem(itemId));
    },
    [withOptimistic],
  );

  const handleApplyDiscount = React.useCallback(async (code: string) => {
    const updated = await applyDiscountCode(code);
    setCart(updated);
  }, []);

  const handleRemoveDiscount = React.useCallback(async () => {
    const updated = await removeDiscount();
    setCart(updated);
  }, []);

  const handleClearCart = React.useCallback(() => {
    // Server-owned sessions can't be revoked from the browser. Clearing the
    // local state is enough for UX; the next page load (or explicit
    // post-checkout redirect) will pick up whatever the server now returns.
    setCart(null);
  }, []);

  const value = React.useMemo<CartContextValue>(
    () => ({
      cart,
      isLoading,
      itemCount,
      addItem: handleAddItem,
      updateItem: handleUpdateItem,
      removeItem: handleRemoveItem,
      applyDiscount: handleApplyDiscount,
      removeDiscountCode: handleRemoveDiscount,
      clearCart: handleClearCart,
    }),
    [
      cart,
      isLoading,
      itemCount,
      handleAddItem,
      handleUpdateItem,
      handleRemoveItem,
      handleApplyDiscount,
      handleRemoveDiscount,
      handleClearCart,
    ],
  );

  return React.createElement(CartContext.Provider, { value }, children);
}

/** Access cart state and actions. Must be used within a CartProvider. */
export function useCart(): CartContextValue {
  const ctx = React.useContext(CartContext);
  if (!ctx) {
    throw new Error("useCart must be used within a <CartProvider>");
  }
  return ctx;
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------

/** Fetch reviews for a product by slug. */
export async function getProductReviews(slug: string): Promise<ReviewsResult> {
  const res = await fetch(
    "/api/reviews?slug=" + encodeURIComponent(slug),
    { credentials: "same-origin" },
  );
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
  return res.json();
}

/** Submit a review for a product. */
export async function submitReview(
  slug: string,
  data: {
    customer_name: string;
    customer_email: string;
    rating: number;
    title?: string;
    body?: string;
  },
): Promise<{ id: string; status: string; message: string }> {
  const res = await fetch("/api/reviews", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ slug, ...data }),
  });
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
  return res.json();
}

/** Hook: fetch and manage reviews for a product. */
export function useReviews(slug: string) {
  const [reviews, setReviews] = React.useState<ProductReview[]>([]);
  const [averageRating, setAverageRating] = React.useState(0);
  const [reviewCount, setReviewCount] = React.useState(0);
  const [isLoading, setIsLoading] = React.useState(true);

  const fetchReviews = React.useCallback(async () => {
    if (!slug) return;
    try {
      const result = await getProductReviews(slug);
      setReviews(result.reviews || []);
      setAverageRating(result.average_rating);
      setReviewCount(result.review_count);
    } catch {
      // silently handle
    } finally {
      setIsLoading(false);
    }
  }, [slug]);

  React.useEffect(() => {
    fetchReviews();
  }, [fetchReviews]);

  const handleSubmitReview = React.useCallback(
    async (data: {
      customer_name: string;
      customer_email: string;
      rating: number;
      title?: string;
      body?: string;
    }) => {
      await submitReview(slug, data);
      await fetchReviews();
    },
    [slug, fetchReviews],
  );

  return {
    reviews,
    averageRating,
    reviewCount,
    isLoading,
    submitReview: handleSubmitReview,
  };
}

// ---------------------------------------------------------------------------
// Wishlist
// ---------------------------------------------------------------------------

// Wishlist uses the same httpOnly wemob_cart_session cookie as the cart —
// the browser carries it transparently on every request.

/** Fetch wishlist items for the current session. */
export async function getWishlist(): Promise<WishlistItem[]> {
  return apiFetch<WishlistItem[]>("/api/wishlist");
}

/** Add a product to the wishlist. */
export async function addToWishlist(productId: string): Promise<void> {
  await apiFetch<{ ok: true }>("/api/wishlist", {
    method: "POST",
    body: JSON.stringify({ productId }),
  });
}

/** Remove a product from the wishlist. */
export async function removeFromWishlist(productId: string): Promise<void> {
  await apiFetch<{ ok: true }>(
    "/api/wishlist/" + encodeURIComponent(productId),
    { method: "DELETE" },
  );
}

/** Hook: manage wishlist state for the current session. */
export function useWishlist() {
  const [items, setItems] = React.useState<WishlistItem[]>([]);
  const [isLoading, setIsLoading] = React.useState(true);

  React.useEffect(() => {
    getWishlist()
      .then((data) => setItems(data || []))
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, []);

  const isInWishlist = React.useCallback(
    (productId: string) => items.some((item) => item.product_id === productId),
    [items],
  );

  const toggleWishlist = React.useCallback(
    async (productId: string) => {
      if (items.some((item) => item.product_id === productId)) {
        await removeFromWishlist(productId);
        setItems((prev) =>
          prev.filter((item) => item.product_id !== productId),
        );
      } else {
        await addToWishlist(productId);
        const updated = await getWishlist();
        setItems(updated || []);
      }
    },
    [items],
  );

  return { items, isLoading, isInWishlist, toggleWishlist };
}
