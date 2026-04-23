"use client";
// lib/wemob-store.ts — Wemob Storefront SDK
// This file is template-managed. Do not modify.
// All exports are client-side only (hooks, context, and async fetch functions).

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
// Environment
// ---------------------------------------------------------------------------

const API_URL = process.env.NEXT_PUBLIC_WEMOB_API_URL || "";
const STORE_ID = process.env.NEXT_PUBLIC_STORE_ID || "";

// ---------------------------------------------------------------------------
// API Fetch Helper
// ---------------------------------------------------------------------------

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const url = API_URL + "/v2/storefront/" + STORE_ID + path;
  const res = await fetch(url, {
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
// Store Config
// ---------------------------------------------------------------------------

/** Fetch the store configuration (name, currency, locale, tax, etc.). */
export async function getStoreConfig(): Promise<StoreConfig> {
  return apiFetch<StoreConfig>("/config");
}

// ---------------------------------------------------------------------------
// Products — async functions for use in client components or route handlers
// ---------------------------------------------------------------------------

/** Fetch products. Optionally filter by category slug or search query. */
export async function getProducts(params?: {
  category?: string;
  q?: string;
}): Promise<StorefrontProduct[]> {
  const query = new URLSearchParams();
  if (params?.category) query.set("category", params.category);
  if (params?.q) query.set("q", params.q);
  const qs = query.toString();
  return apiFetch<StorefrontProduct[]>("/products" + (qs ? "?" + qs : ""));
}

/** Fetch a single product by slug. */
export async function getProduct(slug: string): Promise<StorefrontProduct> {
  return apiFetch<StorefrontProduct>("/products/" + slug);
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

/** Fetch all categories. */
export async function getCategories(): Promise<StorefrontCategory[]> {
  return apiFetch<StorefrontCategory[]>("/categories");
}

// ---------------------------------------------------------------------------
// Cart API
//
// Cart mutations now target local Next.js route handlers that talk directly
// to the tenant's Neon DB. The browser automatically sends the httpOnly
// `wemob_cart_session` cookie owned by those routes, so the legacy
// `sessionId` parameter is ignored by the HTTP layer. We keep the argument
// in the function signatures so existing `CartProvider` callers compile
// unchanged.
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

async function localCartFetch(path: string, options?: RequestInit): Promise<StorefrontCart> {
  const res = await fetch(path, {
    // Same-origin fetch: browser sends the wemob_cart_session cookie
    // automatically. credentials:"same-origin" is the default but we're
    // explicit for clarity.
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...options?.headers },
    ...options,
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error("API error " + res.status + ": " + body);
  }
  const data = (await res.json()) as StorefrontCart | null;
  return data ?? emptyCart();
}

/**
 * Legacy no-op: carts are now created server-side lazily on first add-item.
 * Returns an empty StorefrontCart so any caller that expects a Promise of
 * one still resolves cleanly.
 */
export async function createCart(_sessionId: string): Promise<StorefrontCart> {
  return emptyCart();
}

export async function getCart(_sessionId: string): Promise<StorefrontCart> {
  return localCartFetch("/api/cart");
}

export async function addCartItem(
  _sessionId: string,
  productId: string,
  quantity: number
): Promise<StorefrontCart> {
  return localCartFetch("/api/cart", {
    method: "POST",
    body: JSON.stringify({ productId, quantity }),
  });
}

export async function updateCartItem(
  _sessionId: string,
  itemId: string,
  quantity: number
): Promise<StorefrontCart> {
  return localCartFetch("/api/cart/" + encodeURIComponent(itemId), {
    method: "PUT",
    body: JSON.stringify({ quantity }),
  });
}

export async function removeCartItem(
  _sessionId: string,
  itemId: string
): Promise<StorefrontCart> {
  return localCartFetch("/api/cart/" + encodeURIComponent(itemId), {
    method: "DELETE",
  });
}

export async function applyDiscountCode(
  _sessionId: string,
  code: string
): Promise<StorefrontCart> {
  return localCartFetch("/api/discount", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

/** Remove the currently applied discount code from the cart. */
export async function removeDiscount(_sessionId: string): Promise<StorefrontCart> {
  return localCartFetch("/api/discount", {
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

// ---------------------------------------------------------------------------
// Session Management (cookie + localStorage, SSR-safe)
// ---------------------------------------------------------------------------

const SESSION_COOKIE = "wemob_session";
const SESSION_LS_KEY = "wemob_session_id";
const SESSION_MAX_AGE = 30 * 24 * 60 * 60; // 30 days in seconds

function isBrowser(): boolean {
  return typeof window !== "undefined";
}

function setCookie(name: string, value: string, maxAge: number): void {
  if (!isBrowser()) return;
  document.cookie =
    name +
    "=" +
    encodeURIComponent(value) +
    "; path=/; max-age=" +
    maxAge +
    "; SameSite=Lax";
}

function getCookie(name: string): string | null {
  if (!isBrowser()) return null;
  const match = document.cookie.match(
    new RegExp("(?:^|; )" + name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "=([^;]*)")
  );
  return match ? decodeURIComponent(match[1]) : null;
}

function deleteCookie(name: string): void {
  if (!isBrowser()) return;
  document.cookie = name + "=; path=/; max-age=0; SameSite=Lax";
}

/** Get or create a persistent session ID, stored in both cookie and localStorage. */
export function getSessionId(): string {
  if (!isBrowser()) return "";

  // Try cookie first, then localStorage
  let id = getCookie(SESSION_COOKIE);
  if (!id) {
    id = localStorage.getItem(SESSION_LS_KEY);
  }
  if (!id) {
    id = (typeof crypto !== "undefined" && crypto.randomUUID) ? crypto.randomUUID() : (Math.random().toString(36).slice(2) + Date.now().toString(36));
  }

  // Persist to both stores
  setCookie(SESSION_COOKIE, id, SESSION_MAX_AGE);
  try {
    localStorage.setItem(SESSION_LS_KEY, id);
  } catch {
    // localStorage may be unavailable (private browsing, quota)
  }

  return id;
}

/** Clear all session data. */
export function clearSession(): void {
  if (!isBrowser()) return;
  deleteCookie(SESSION_COOKIE);
  try {
    localStorage.removeItem(SESSION_LS_KEY);
  } catch {
    // ignore
  }
}

// ---------------------------------------------------------------------------
// Cart Cache (localStorage, SSR-safe)
// ---------------------------------------------------------------------------

const CART_CACHE_KEY = "wemob_cart_" + STORE_ID;

interface CartCache { cart: StorefrontCart; ts: number; }

function saveCartCache(cart: StorefrontCart | null): void {
  if (!isBrowser()) return;
  try {
    if (cart && cart.items && cart.items.length > 0) {
      localStorage.setItem(CART_CACHE_KEY, JSON.stringify({ cart, ts: Date.now() }));
    } else {
      localStorage.removeItem(CART_CACHE_KEY);
    }
  } catch { /* localStorage unavailable */ }
}

function loadCartCache(): StorefrontCart | null {
  if (!isBrowser()) return null;
  try {
    const raw = localStorage.getItem(CART_CACHE_KEY);
    if (!raw) return null;
    const cached: CartCache = JSON.parse(raw);
    const sid = getCookie(SESSION_COOKIE) || localStorage.getItem(SESSION_LS_KEY);
    if (cached.cart.session_id && sid && cached.cart.session_id !== sid) {
      localStorage.removeItem(CART_CACHE_KEY);
      return null;
    }
    return cached.cart;
  } catch {
    try { localStorage.removeItem(CART_CACHE_KEY); } catch { /* ignore */ }
    return null;
  }
}

function clearCartCache(): void {
  if (!isBrowser()) return;
  try { localStorage.removeItem(CART_CACHE_KEY); } catch { /* ignore */ }
}

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
  const sessionIdRef = React.useRef<string>("");
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

  // Initialize cart from session on mount
  React.useEffect(() => {
    let cancelled = false;

    async function init() {
      const sid = getSessionId();
      if (!sid) { setIsLoading(false); return; }
      sessionIdRef.current = sid;

      // Hydrate from cache immediately
      const cached = loadCartCache();
      if (cached) {
        setCart(cached);
        setIsLoading(false);
      }

      // Revalidate from API in background
      try {
        const existing = await getCart(sid);
        if (!cancelled) { setCart(existing); saveCartCache(existing); }
      } catch {
        if (!cached) {
          try {
            const fresh = await createCart(sid);
            if (!cancelled) { setCart(fresh); saveCartCache(fresh); }
          } catch { /* API unreachable, stay with cache or empty */ }
        }
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
        saveCartCache(updated);
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
      const sid = sessionIdRef.current || getSessionId();
      sessionIdRef.current = sid;
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

      await withOptimistic(optimistic, () =>
        addCartItem(sid, productId, qty)
      );
    },
    [withOptimistic]
  );

  const handleUpdateItem = React.useCallback(
    async (itemId: string, quantity: number) => {
      const sid = sessionIdRef.current;
      const current = cartRef.current;
      if (!sid || !current) return;

      // Optimistic: update quantity in-place and recalculate totals
      const optimistic = recalcTotals({
        ...current,
        items: current.items.map((item) =>
          item.id === itemId
            ? { ...item, quantity, total_cents: item.price_cents * quantity }
            : item
        ),
      });

      await withOptimistic(optimistic, () =>
        updateCartItem(sid, itemId, quantity)
      );
    },
    [withOptimistic]
  );

  const handleRemoveItem = React.useCallback(
    async (itemId: string) => {
      const sid = sessionIdRef.current;
      const current = cartRef.current;
      if (!sid || !current) return;

      // Optimistic: remove item and recalculate totals
      const optimistic = recalcTotals({
        ...current,
        items: current.items.filter((item) => item.id !== itemId),
      });

      await withOptimistic(optimistic, () =>
        removeCartItem(sid, itemId)
      );
    },
    [withOptimistic]
  );

  const handleApplyDiscount = React.useCallback(
    async (code: string) => {
      const sid = sessionIdRef.current;
      if (!sid) return;

      const updated = await applyDiscountCode(sid, code);
      setCart(updated);
      saveCartCache(updated);
    },
    []
  );

  const handleRemoveDiscount = React.useCallback(
    async () => {
      const sid = sessionIdRef.current;
      if (!sid) return;

      const updated = await removeDiscount(sid);
      setCart(updated);
      saveCartCache(updated);
    },
    []
  );

  const handleClearCart = React.useCallback(() => {
    setCart(null);
    clearCartCache();
    clearSession();
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

/** Fetch wishlist items for the current session. */
export async function getWishlist(_sessionId: string): Promise<WishlistItem[]> {
  const res = await fetch("/api/wishlist", { credentials: "same-origin" });
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
  return res.json();
}

/** Add a product to the wishlist. */
export async function addToWishlist(_sessionId: string, productId: string): Promise<void> {
  const res = await fetch("/api/wishlist", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ productId }),
  });
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
}

/** Remove a product from the wishlist. */
export async function removeFromWishlist(_sessionId: string, productId: string): Promise<void> {
  const res = await fetch("/api/wishlist/" + encodeURIComponent(productId), {
    method: "DELETE",
    credentials: "same-origin",
  });
  if (!res.ok) {
    throw new Error("API error " + res.status + ": " + (await res.text()));
  }
}

/** Hook: manage wishlist state for the current session. */
export function useWishlist() {
  const [items, setItems] = React.useState<WishlistItem[]>([]);
  const [isLoading, setIsLoading] = React.useState(true);

  React.useEffect(() => {
    const sid = getSessionId();
    if (!sid) { setIsLoading(false); return; }

    getWishlist(sid)
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
      const sid = getSessionId();
      if (!sid) return;

      if (items.some((item) => item.product_id === productId)) {
        await removeFromWishlist(sid, productId);
        setItems((prev) => prev.filter((item) => item.product_id !== productId));
      } else {
        await addToWishlist(sid, productId);
        const updated = await getWishlist(sid);
        setItems(updated || []);
      }
    },
    [items]
  );

  return { items, isLoading, isInWishlist, toggleWishlist };
}
