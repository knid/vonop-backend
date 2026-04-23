"use client";
// lib/wemob-store.ts — Wemob Storefront SDK
// This file is template-managed. Do not modify.
// All exports are client-side only (hooks, context, and async helpers).

import React from "react";
import {
  getStoreConfigAction,
  getProductsAction,
  getProductAction,
  getCategoriesAction,
} from "./wemob-storefront-actions";
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
//
// Reads go through React Server Actions (wemob-storefront-actions.ts) — no
// bespoke /api/* route per resource. Mutations still POST to dedicated
// /api/cart, /api/discount, /api/wishlist, /api/reviews, /api/checkout,
// /api/contact routes because those have real business logic beyond a thin
// wrapper and benefit from explicit HTTP status codes.
//
// The browser carries the httpOnly wemob_cart_session cookie on every
// request; there is no client-side session id to pass around.

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
// Store reads (server actions)
// ---------------------------------------------------------------------------

/** Fetch the store configuration (name, currency, locale, tax, etc.). */
export async function getStoreConfig(): Promise<StoreConfig> {
  return getStoreConfigAction();
}

/** Fetch products. Optionally filter by category slug or search query. */
export async function getProducts(params?: {
  category?: string;
  q?: string;
}): Promise<StorefrontProduct[]> {
  return getProductsAction(params);
}

/** Fetch a single product by slug. Returns null when no such product exists. */
export async function getProduct(
  slug: string,
): Promise<StorefrontProduct | null> {
  return getProductAction(slug);
}

/** Fetch all categories. */
export async function getCategories(): Promise<StorefrontCategory[]> {
  return getCategoriesAction();
}

// ---------------------------------------------------------------------------
// Cart API
//
// Cart mutations target local Next.js route handlers that talk directly
// to the tenant's Neon DB. The browser automatically sends the httpOnly
// wemob_cart_session cookie owned by those routes — that is the SINGLE
// source of truth for which cart belongs to this browser.
// ---------------------------------------------------------------------------

// Build an empty StorefrontCart so provider state stays well-shaped when
// the server has no cart row yet (e.g. before the first add-to-cart).
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

async function cartFetch(path: string, options?: RequestInit): Promise<StorefrontCart> {
  try {
    const data = await apiFetch<StorefrontCart | null>(path, options);
    return data ?? emptyCart();
  } catch (err) {
    throw err;
  }
}

export async function getCart(): Promise<StorefrontCart> {
  return cartFetch("/api/cart");
}

export async function addCartItem(
  productId: string,
  quantity: number
): Promise<StorefrontCart> {
  return cartFetch("/api/cart", {
    method: "POST",
    body: JSON.stringify({ productId, quantity }),
  });
}

export async function updateCartItem(
  itemId: string,
  quantity: number
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
// Checkout
//
// The checkout submit flow now POSTs directly to the local /api/checkout
// route handler, which creates the order row server-side (using the Neon DB)
// and invokes the selected payment adapter. The form calls fetch() inline
// rather than going through a helper here — the response shape is tied to
// `CheckoutResult` in lib/payments/index.ts (a server-only module we can't
// import from the client-side SDK), so colocating the type with its one
// caller keeps this file provider-neutral.
//
// Order reads (success/cancel pages) go through `getOrder` in
// wemob-store-server.ts — not re-exported here since client components
// shouldn't read orders directly.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Price Utilities
// ---------------------------------------------------------------------------

let _configCache: StoreConfig | null = null;

/**
 * Format a price in cents to a human-readable currency string.
 * Uses the store config currency/locale when available.
 */
export function formatPrice(cents: number, currency?: string): string {
  const cur = currency || (_configCache ? _configCache.currency : "USD");
  const locale = _configCache ? _configCache.locale : "en-US";
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency: cur,
  }).format(cents / 100);
}

// No localStorage mirror of cart state: the httpOnly wemob_cart_session
// cookie + the server's cart row is the single source of truth. An earlier
// revision cached the cart to paint the badge instantly, but that cache
// outlived the server row after a successful checkout and produced a badge
// that disagreed with the cart page. The badge now waits for /api/cart;
// itemCount>0 gates rendering so the blank period is invisible.

// ---------------------------------------------------------------------------
// Internal: recalculate cart totals from items
// ---------------------------------------------------------------------------

function recalcTotals(c: StorefrontCart): StorefrontCart {
  const subtotal_cents = c.items.reduce((sum, i) => sum + i.price_cents * i.quantity, 0);
  return {
    ...c,
    subtotal_cents,
    total_cents: subtotal_cents - c.discount_cents,
  };
}

// ---------------------------------------------------------------------------
// Cart Context
// ---------------------------------------------------------------------------

interface CartContextValue {
  cart: StorefrontCart | null;
  isLoading: boolean;
  itemCount: number;
  addItem: (productId: string, quantity?: number) => Promise<void>;
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

  // Keep ref in sync with state
  React.useEffect(() => {
    cartRef.current = cart;
  }, [cart]);

  // Compute item count
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

  // --- Optimistic helper (uses ref to avoid stale closures) ---

  const withOptimistic = React.useCallback(
    async (
      optimisticCart: StorefrontCart | null,
      apiCall: () => Promise<StorefrontCart>
    ) => {
      const previous = cartRef.current;
      if (optimisticCart) setCart(optimisticCart);

      try {
        const updated = await apiCall();
        setCart(updated);
      } catch (err) {
        // Rollback on failure
        setCart(previous);
        throw err;
      }
    },
    []
  );

  // --- Actions ---

  const handleAddItem = React.useCallback(
    async (productId: string, quantity?: number) => {
      const qty = Math.max(1, quantity ?? 1);

      // Optimistic: add a placeholder item and recalculate totals
      const current = cartRef.current;
      const optimistic = current
        ? recalcTotals({
            ...current,
            items: [
              ...current.items,
              {
                id: "__optimistic__" + Date.now(),
                product_id: productId,
                product_name: "",
                product_image: "",
                quantity: qty,
                price_cents: 0,
                total_cents: 0,
              },
            ],
          })
        : null;

      await withOptimistic(optimistic, () => addCartItem(productId, qty));
    },
    [withOptimistic]
  );

  const handleUpdateItem = React.useCallback(
    async (itemId: string, quantity: number) => {
      const current = cartRef.current;
      if (!current) return;

      // Optimistic: update quantity in-place and recalculate totals
      const optimistic = recalcTotals({
        ...current,
        items: current.items.map((item) =>
          item.id === itemId
            ? { ...item, quantity, total_cents: item.price_cents * quantity }
            : item
        ),
      });

      await withOptimistic(optimistic, () => updateCartItem(itemId, quantity));
    },
    [withOptimistic]
  );

  const handleRemoveItem = React.useCallback(
    async (itemId: string) => {
      const current = cartRef.current;
      if (!current) return;

      // Optimistic: remove item and recalculate totals
      const optimistic = recalcTotals({
        ...current,
        items: current.items.filter((item) => item.id !== itemId),
      });

      await withOptimistic(optimistic, () => removeCartItem(itemId));
    },
    [withOptimistic]
  );

  const handleApplyDiscount = React.useCallback(
    async (code: string) => {
      const updated = await applyDiscountCode(code);
      setCart(updated);
    },
    []
  );

  const handleRemoveDiscount = React.useCallback(
    async () => {
      const updated = await removeDiscount();
      setCart(updated);
    },
    []
  );

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
    ]
  );

  return React.createElement(CartContext.Provider, { value }, children);
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

/** Access cart state and actions. Must be used within a CartProvider. */
export function useCart(): CartContextValue {
  const ctx = React.useContext(CartContext);
  if (!ctx) {
    throw new Error("useCart must be used within a <CartProvider>");
  }
  return ctx;
}

/** Fetch and return products. Optionally filter by category slug or search query. */
export function useProducts(params?: { category?: string; q?: string }): {
  products: StorefrontProduct[];
  isLoading: boolean;
  error: Error | null;
} {
  const [products, setProducts] = React.useState<StorefrontProduct[]>([]);
  const [isLoading, setIsLoading] = React.useState(true);
  const [error, setError] = React.useState<Error | null>(null);

  const category = params?.category;
  const q = params?.q;

  React.useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);

    getProducts({ category, q })
      .then((data) => {
        if (!cancelled) {
          setProducts(data);
          setIsLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [category, q]);

  return { products, isLoading, error };
}

/** Fetch and return a single product by slug. */
export function useProduct(slug: string): {
  product: StorefrontProduct | null;
  isLoading: boolean;
  error: Error | null;
} {
  const [product, setProduct] = React.useState<StorefrontProduct | null>(null);
  const [isLoading, setIsLoading] = React.useState(true);
  const [error, setError] = React.useState<Error | null>(null);

  React.useEffect(() => {
    if (!slug) {
      setProduct(null);
      setIsLoading(false);
      return;
    }

    let cancelled = false;
    setIsLoading(true);
    setError(null);

    getProduct(slug)
      .then((data) => {
        if (!cancelled) {
          setProduct(data);
          setIsLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [slug]);

  return { product, isLoading, error };
}

/** Fetch and return all categories. */
export function useCategories(): {
  categories: StorefrontCategory[];
  isLoading: boolean;
  error: Error | null;
} {
  const [categories, setCategories] = React.useState<StorefrontCategory[]>([]);
  const [isLoading, setIsLoading] = React.useState(true);
  const [error, setError] = React.useState<Error | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);

    getCategories()
      .then((data) => {
        if (!cancelled) {
          setCategories(data);
          setIsLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return { categories, isLoading, error };
}

/** Fetch and return the store configuration. Also warms the formatPrice cache. */
export function useStoreConfig(): {
  config: StoreConfig | null;
  isLoading: boolean;
  error: Error | null;
} {
  const [config, setConfig] = React.useState<StoreConfig | null>(null);
  const [isLoading, setIsLoading] = React.useState(true);
  const [error, setError] = React.useState<Error | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);

    getStoreConfig()
      .then((data) => {
        if (!cancelled) {
          _configCache = data;
          setConfig(data);
          setIsLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return { config, isLoading, error };
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------

/** Fetch reviews for a product by slug. */
export async function getProductReviews(slug: string): Promise<ReviewsResult> {
  const res = await fetch(
    "/api/reviews?slug=" + encodeURIComponent(slug),
    { credentials: "same-origin" }
  );
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
  return res.json();
}

/** Submit a review for a product. */
export async function submitReview(
  slug: string,
  data: { customer_name: string; customer_email: string; rating: number; title?: string; body?: string }
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
    async (data: { customer_name: string; customer_email: string; rating: number; title?: string; body?: string }) => {
      await submitReview(slug, data);
      // Refresh reviews after submission
      await fetchReviews();
    },
    [slug, fetchReviews]
  );

  return { reviews, averageRating, reviewCount, isLoading, submitReview: handleSubmitReview };
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
  await apiFetch<{ ok: true }>("/api/wishlist/" + encodeURIComponent(productId), {
    method: "DELETE",
  });
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
    [items]
  );

  const toggleWishlist = React.useCallback(
    async (productId: string) => {
      if (items.some((item) => item.product_id === productId)) {
        await removeFromWishlist(productId);
        setItems((prev) => prev.filter((item) => item.product_id !== productId));
      } else {
        await addToWishlist(productId);
        const updated = await getWishlist();
        setItems(updated || []);
      }
    },
    [items]
  );

  return { items, isLoading, isInWishlist, toggleWishlist };
}
