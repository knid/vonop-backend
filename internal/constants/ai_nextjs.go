package constants

const DEFAULT_NEXTJS_LLM_MODEL = "anthropic/claude-sonnet-4"

// Template file paths that are managed by hardcoded templates, not LLM-generated.
var NEXTJS_TEMPLATE_FILE_PATHS = []string{
	"package.json",
	"tsconfig.json",
	"postcss.config.mjs",
	"next.config.mjs",
	"app/globals.css",
}

// NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS are template-managed files injected only for e-commerce projects.
var NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS = []string{
	"lib/wemob-store.ts",
}

const NEXTJS_TEMPLATE_PACKAGE_JSON = `{
  "name": "app",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint ."
  },
  "dependencies": {
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "lucide-react": "^0.460.0",
    "clsx": "^2.1.0",
    "tailwind-merge": "^2.6.0",
    "class-variance-authority": "^0.7.0",
    "framer-motion": "^11.0.0",
    "@radix-ui/react-slot": "^1.1.0",
    "@radix-ui/react-dialog": "^1.1.0",
    "@radix-ui/react-dropdown-menu": "^2.1.0",
    "@radix-ui/react-tabs": "^1.1.0",
    "@radix-ui/react-tooltip": "^1.1.0",
    "@radix-ui/react-avatar": "^1.1.0",
    "@radix-ui/react-separator": "^1.1.0",
    "@radix-ui/react-scroll-area": "^1.2.0",
    "@radix-ui/react-switch": "^1.1.0",
    "@radix-ui/react-select": "^2.1.0",
    "@radix-ui/react-accordion": "^1.2.0",
    "@radix-ui/react-popover": "^1.1.0"
  },
  "devDependencies": {
    "typescript": "^5.4.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@types/node": "^20.0.0",
    "tailwindcss": "^4.0.0",
    "@tailwindcss/postcss": "^4.0.0",
    "postcss": "^8.4.0"
  }
}`

const NEXTJS_TEMPLATE_TSCONFIG = `{
  "compilerOptions": {
    "lib": ["dom", "dom.iterable", "esnext"],
    "allowJs": true,
    "skipLibCheck": true,
    "strict": true,
    "noEmit": true,
    "esModuleInterop": true,
    "module": "esnext",
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "isolatedModules": true,
    "jsx": "react-jsx",
    "incremental": true,
    "plugins": [
      {
        "name": "next"
      }
    ],
    "paths": {
      "@/*": ["./*"]
    }
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx", ".next/types/**/*.ts"],
  "exclude": ["node_modules"]
}`

const NEXTJS_TEMPLATE_POSTCSS_CONFIG = `/** @type {import('postcss-load-config').Config} */
const config = {
  plugins: {
    "@tailwindcss/postcss": {},
  },
};

export default config;
`

const NEXTJS_TEMPLATE_NEXT_CONFIG = `/** @type {import('next').NextConfig} */
const nextConfig = {
  images: {
    remotePatterns: [
      {
        protocol: "https",
        hostname: "**",
      },
    ],
  },
};

export default nextConfig;
`

// NEXTJS_TEMPLATE_WEMOB_STORE is the typed storefront SDK with React context, hooks, and API client
// injected for e-commerce projects. No JS template literals are used, so a single Go raw string works.
const NEXTJS_TEMPLATE_WEMOB_STORE = `"use client";
// lib/wemob-store.ts — Wemob Storefront SDK
// This file is template-managed. Do not modify.
// All exports are client-side only (hooks, context, and async fetch functions).

import React from "react";

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

const API_URL = process.env.NEXT_PUBLIC_WEMOB_API_URL || "";
const STORE_ID = process.env.NEXT_PUBLIC_STORE_ID || "";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface StoreConfig {
  name: string;
  currency: string;
  locale: string;
  tax_rate: number;
  payment_providers: string[];
  shipping_enabled: boolean;
}

export interface StorefrontProduct {
  id: string;
  name: string;
  slug: string;
  description: string;
  images: string[];
  category: string;
  tags: string[];
  variants: StorefrontVariant[];
  min_price_cents: number;
  max_price_cents: number;
}

export interface StorefrontVariant {
  id: string;
  name: string;
  price_in_cents: number;
  compare_at_price: number;
  inventory: number;
  in_stock: boolean;
  options: Record<string, unknown>;
}

export interface StorefrontCategory {
  id: string;
  name: string;
  slug: string;
  image: string;
  description: string;
}

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
  variant_id: string;
  variant_name: string;
  quantity: number;
  price_cents: number;
  total_cents: number;
}

export interface StorefrontCheckout {
  order_id: string;
  order_number: string;
  total_cents: number;
  currency: string;
  payment_url?: string;
  client_secret?: string;
}

export interface CheckoutConfirm {
  order_id: string;
  order_number: string;
  status: string;
}

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
  variant_name: string;
  quantity: number;
  price_in_cents: number;
  total_cents: number;
}

export interface ShippingAddress {
  line1: string;
  line2?: string;
  city: string;
  state: string;
  postal_code: string;
  country: string;
}

export interface CheckoutParams {
  sessionId: string;
  customerEmail: string;
  customerName: string;
  shippingAddress: ShippingAddress;
  billingAddress?: ShippingAddress;
  paymentProvider: string;
}

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
// ---------------------------------------------------------------------------

export async function createCart(sessionId: string): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart", {
    method: "POST",
    body: JSON.stringify({ session_id: sessionId }),
  });
}

export async function getCart(sessionId: string): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId);
}

export async function addCartItem(
  sessionId: string,
  productId: string,
  variantId: string,
  quantity: number
): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/items", {
    method: "POST",
    body: JSON.stringify({ product_id: productId, variant_id: variantId, quantity }),
  });
}

export async function updateCartItem(
  sessionId: string,
  itemId: string,
  quantity: number
): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/items/" + itemId, {
    method: "PUT",
    body: JSON.stringify({ quantity }),
  });
}

export async function removeCartItem(
  sessionId: string,
  itemId: string
): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/items/" + itemId, {
    method: "DELETE",
  });
}

export async function applyDiscountCode(
  sessionId: string,
  code: string
): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/discount", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

/** Remove the currently applied discount code from the cart. */
export async function removeDiscount(sessionId: string): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/discount", {
    method: "DELETE",
  });
}

// ---------------------------------------------------------------------------
// Checkout API
// ---------------------------------------------------------------------------

/** Create a checkout session. Returns payment info for redirect or form rendering. */
export async function checkout(params: CheckoutParams): Promise<StorefrontCheckout> {
  return apiFetch<StorefrontCheckout>("/checkout", {
    method: "POST",
    body: JSON.stringify({
      session_id: params.sessionId,
      customer_email: params.customerEmail,
      customer_name: params.customerName,
      shipping_address: params.shippingAddress,
      billing_address: params.billingAddress,
      payment_provider: params.paymentProvider,
    }),
  });
}

/** Confirm payment after redirect. */
export async function confirmCheckout(
  orderId: string,
  paymentId: string
): Promise<CheckoutConfirm> {
  return apiFetch<CheckoutConfirm>("/checkout/confirm", {
    method: "POST",
    body: JSON.stringify({ order_id: orderId, payment_id: paymentId }),
  });
}

// ---------------------------------------------------------------------------
// Order Tracking
// ---------------------------------------------------------------------------

/** Fetch order details for tracking / confirmation page. */
export async function getOrder(orderId: string): Promise<OrderTracking> {
  return apiFetch<OrderTracking>("/orders/" + orderId);
}

// ---------------------------------------------------------------------------
// Payment Form Helper (Iyzico)
// ---------------------------------------------------------------------------

/**
 * Safely render an Iyzico checkout form (HTML string with embedded scripts)
 * into a container element. Uses DOMParser for safe HTML parsing, then
 * re-executes script tags so the Iyzico JS runs correctly.
 */
export function renderPaymentForm(container: HTMLElement, htmlContent: string): void {
  // Clear previous content safely
  while (container.firstChild) {
    container.removeChild(container.firstChild);
  }

  // Parse HTML safely via DOMParser
  const parser = new DOMParser();
  const doc = parser.parseFromString(htmlContent, "text/html");

  // Adopt nodes into the current document
  const fragment = document.createDocumentFragment();
  Array.from(doc.body.childNodes).forEach((node) => {
    const adopted = document.adoptNode(node);
    fragment.appendChild(adopted);
  });
  container.appendChild(fragment);

  // Re-execute script tags (adopted scripts don't execute automatically)
  const scripts = container.querySelectorAll("script");
  scripts.forEach((oldScript) => {
    const newScript = document.createElement("script");
    Array.from(oldScript.attributes).forEach((attr) => {
      newScript.setAttribute(attr.name, attr.value);
    });
    newScript.textContent = oldScript.textContent;
    oldScript.parentNode?.replaceChild(newScript, oldScript);
  });
}

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
  addItem: (productId: string, variantId?: string, quantity?: number) => Promise<void>;
  updateItem: (itemId: string, quantity: number) => Promise<void>;
  removeItem: (itemId: string) => Promise<void>;
  applyDiscount: (code: string) => Promise<void>;
  removeDiscountCode: () => Promise<void>;
  checkout: (params: CheckoutParams) => Promise<StorefrontCheckout>;
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
      if (!sid) {
        setIsLoading(false);
        return;
      }
      sessionIdRef.current = sid;

      try {
        const existing = await getCart(sid);
        if (!cancelled) setCart(existing);
      } catch {
        // Cart does not exist yet — create one
        try {
          const fresh = await createCart(sid);
          if (!cancelled) setCart(fresh);
        } catch {
          // API unreachable; start with empty state
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
    async (productId: string, variantId?: string, quantity?: number) => {
      const sid = sessionIdRef.current || getSessionId();
      sessionIdRef.current = sid;
      const qty = Math.max(1, quantity ?? 1);
      const vid = variantId ?? "";

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
                variant_id: vid,
                variant_name: "",
                quantity: qty,
                price_cents: 0,
                total_cents: 0,
              },
            ],
          })
        : null;

      await withOptimistic(optimistic, () =>
        addCartItem(sid, productId, vid, qty)
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
    },
    []
  );

  const handleRemoveDiscount = React.useCallback(
    async () => {
      const sid = sessionIdRef.current;
      if (!sid) return;

      const updated = await removeDiscount(sid);
      setCart(updated);
    },
    []
  );

  const handleCheckout = React.useCallback(
    async (params: CheckoutParams): Promise<StorefrontCheckout> => {
      const result = await checkout({
        ...params,
        sessionId: params.sessionId || sessionIdRef.current,
      });

      // Auto-redirect to hosted payment page (Stripe) when available
      if (result.payment_url && isBrowser()) {
        window.location.href = result.payment_url;
        return result;
      }

      // Iyzico: client_secret contains checkout form HTML — don't redirect,
      // let the caller render it via renderPaymentForm().

      // No payment provider (COD / manual): clear cart, return result for confirmation page
      if (!result.payment_url && !result.client_secret) {
        setCart(null);
      }

      return result;
    },
    []
  );

  const handleClearCart = React.useCallback(() => {
    setCart(null);
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
      checkout: handleCheckout,
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
      handleCheckout,
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
`

const NEXTJS_CLARIFICATION_PROMPT = `You are an expert product strategist and UX researcher. The user wants to build a web application and has provided a short prompt. Your job is to generate 3-5 context-specific clarifying questions that will help produce a much better, more personalized result.

### Rules
1. Generate questions that extract critical details the user's prompt is missing — for example: target audience, brand style, specific features, content/data they want to showcase, preferred color palette or vibe.
2. Questions should be relevant to the specific prompt, not generic. If the user says "build me a coffee shop website", ask about their menu, location, delivery options — not generic questions like "what is your target audience?".
3. Each question has an id (q1, q2, ...), the question text, a type ("text" for free-form or "choice" for multiple choice), an optional options array (for choice type), and a required boolean.
4. Include at least one "choice" question when it makes sense (e.g. "What style do you prefer?" with options like "Modern & Minimal", "Bold & Colorful", "Elegant & Classic").
5. Keep questions concise and actionable. The user should be able to answer each in a few words or a sentence.
6. Generate 3-5 questions. Prefer fewer, higher-impact questions over many trivial ones.

### Output format (strict JSON, no explanation, no markdown)
{
  "questions": [
    {"id": "q1", "question": "What is your store name and what products will you sell?", "type": "text", "options": [], "required": true},
    {"id": "q2", "question": "What visual style do you prefer?", "type": "choice", "options": ["Modern & Minimal", "Bold & Colorful", "Elegant & Classic", "Playful & Fun"], "required": true},
    {"id": "q3", "question": "Do you need any specific pages besides the homepage?", "type": "text", "options": [], "required": false}
  ]
}`

const NEXTJS_STRUCTURE_PROMPT = `You are an expert Next.js developer, application architect, and UI/UX designer who creates modern, visually stunning web applications.

Given the user's description of a web application they want to build, generate a structured file plan for a Next.js 16 App Router project.

### Design Philosophy
- Design like a top-tier design agency. Think Linear, Vercel, Stripe, Notion level quality.
- Modern, clean, and professional aesthetics. Follow current 2024-2025 design trends.
- Subtle gradients, glass morphism, smooth animations, and micro-interactions.
- Generous whitespace, consistent spacing, and visual hierarchy.
- Dark mode support by default.
- Beautiful typography pairing from Google Fonts.

### Rules
1. Generate an app name and URL-safe slug from the user's prompt.
2. Generate a sophisticated, harmonious color scheme with primary, secondary, accent, background, surface, and text colors (hex values). Avoid generic or flat colors — use refined, modern palettes.
3. Also generate a dark color scheme variant.
4. Choose professional, modern typography pairing (heading_font and body_font from Google Fonts). Avoid overused fonts — consider Inter, Plus Jakarta Sans, DM Sans, Outfit, Sora, Manrope, Space Grotesk.
5. Plan the files needed for the project. Each file has a path, type, and description.
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, or next.config.mjs — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. Plan a lib/utils.ts file that exports a cn() helper using clsx and tailwind-merge.
9. Use Next.js App Router structure (app/ directory for pages).
10. Plan 3-8 pages depending on complexity.
11. Include shared UI components in a components/ directory. Plan reusable primitives (Button, Card, Badge, Input, etc.) in components/ui/ following shadcn/ui patterns.
12. Keep the structure clean and well-organized.
13. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. If page.tsx imports "@/components/project-card", you MUST include "components/project-card.tsx" in the plan. If page.tsx imports "@/lib/projects", you MUST include "lib/projects.ts" in the plan. Missing files will cause build failures.
14. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
15. If the application requires external API keys or secrets (payment gateways, databases, third-party APIs, etc.), list them in "required_env_vars". Each entry: "key" (env var name, e.g. "STRIPE_SK"), "description" (what it's for), "is_public" (true if NEXT_PUBLIC_ prefixed for client-side), "provider" (optional, matching integration: "stripe", "iyzico", "supabase", etc.). Do NOT include NEXT_PUBLIC_STORE_ID or NEXT_PUBLIC_WEMOB_API_URL (auto-injected).

### Output format (strict JSON, no explanation, no markdown)
{
  "name": "App Name",
  "slug": "app-name",
  "colorScheme": {
    "primary": "#hex",
    "secondary": "#hex",
    "accent": "#hex",
    "background": "#hex",
    "surface": "#hex",
    "text": "#hex"
  },
  "darkColorScheme": {
    "primary": "#hex",
    "secondary": "#hex",
    "accent": "#hex",
    "background": "#hex",
    "surface": "#hex",
    "text": "#hex"
  },
  "typography": {
    "heading_font": "Inter",
    "body_font": "Inter"
  },
  "files": [
    {"path": "app/layout.tsx", "type": "tsx", "description": "Root layout with fonts, metadata, and navigation"},
    {"path": "app/page.tsx", "type": "tsx", "description": "Homepage with hero section"},
    {"path": "app/globals.css", "type": "css", "description": "Global styles with Tailwind v4 @theme colors"},
    {"path": "components/header.tsx", "type": "tsx", "description": "Navigation header"},
    {"path": "components/footer.tsx", "type": "tsx", "description": "Footer component"}
  ],
  "required_env_vars": [
    {"key": "STRIPE_SK", "description": "Stripe secret key for payments", "is_public": false, "provider": "stripe"}
  ]
}`

const NEXTJS_MOBILE_STRUCTURE_PROMPT = `You are an expert mobile-first web developer, application architect, and UI/UX designer who creates beautiful, native-feeling mobile web applications.

Given the user's description of a mobile app they want to build, generate a structured file plan for a Next.js 16 App Router project optimized for mobile devices.

### Design Philosophy
- Design like a native mobile app. Think iOS/Android native quality rendered as a responsive web app.
- Mobile-first and mobile-only — target max-width ~430px viewport. No desktop layouts.
- Use mobile UI patterns: bottom tabs, swipe gestures, touch-friendly tap targets (min 44px), pull-to-refresh.
- Smooth animations, spring physics, haptic-like feedback, and native-feeling transitions.
- Bottom sheets instead of modals, slide-up panels, card-based navigation.
- Safe area insets for modern devices. Status bar awareness.
- Dark mode support by default.

### Rules
1. Generate an app name and URL-safe slug from the user's prompt.
2. Generate a sophisticated, harmonious color scheme with primary, secondary, accent, background, surface, and text colors (hex values). Use vibrant, modern mobile-friendly palettes.
3. Also generate a dark color scheme variant.
4. Choose professional, modern typography pairing (heading_font and body_font from Google Fonts). Consider Inter, SF Pro-like alternatives, DM Sans, Plus Jakarta Sans.
5. Plan the files needed for the project. Each file has a path, type, and description.
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, or next.config.mjs — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. Plan a lib/utils.ts file that exports a cn() helper using clsx and tailwind-merge.
9. Use Next.js App Router structure (app/ directory for pages).
10. Plan 3-8 pages depending on complexity.
11. Include shared UI components in a components/ directory. Plan reusable primitives (Button, Card, Badge, Input, etc.) in components/ui/ following shadcn/ui patterns.
12. Keep the structure clean and well-organized.
13. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. Missing files will cause build failures.
14. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
15. MOBILE-SPECIFIC: Include viewport meta tag configuration for mobile. Use touch-friendly sizing. Plan bottom tab navigation component. Use mobile-appropriate components (bottom sheets, slide-up modals, swipeable cards). All layouts must work within 430px max width. NO desktop-only layouts.
16. Generate mobile-first CSS — use Tailwind's default breakpoint as the base (mobile), never require larger breakpoints for core functionality.
17. Include PWA-ready manifest hints in the layout description.
18. If the application requires external API keys or secrets (payment gateways, databases, third-party APIs, etc.), list them in "required_env_vars". Each entry: "key" (env var name, e.g. "STRIPE_SK"), "description" (what it's for), "is_public" (true if NEXT_PUBLIC_ prefixed for client-side), "provider" (optional, matching integration: "stripe", "iyzico", "supabase", etc.). Do NOT include NEXT_PUBLIC_STORE_ID or NEXT_PUBLIC_WEMOB_API_URL (auto-injected).

### Output format (strict JSON, no explanation, no markdown)
{
  "name": "App Name",
  "slug": "app-name",
  "colorScheme": {
    "primary": "#hex",
    "secondary": "#hex",
    "accent": "#hex",
    "background": "#hex",
    "surface": "#hex",
    "text": "#hex"
  },
  "darkColorScheme": {
    "primary": "#hex",
    "secondary": "#hex",
    "accent": "#hex",
    "background": "#hex",
    "surface": "#hex",
    "text": "#hex"
  },
  "typography": {
    "heading_font": "Inter",
    "body_font": "Inter"
  },
  "files": [
    {"path": "app/layout.tsx", "type": "tsx", "description": "Root layout with mobile viewport, fonts, metadata, and bottom navigation"},
    {"path": "app/page.tsx", "type": "tsx", "description": "Home screen with mobile-optimized layout"},
    {"path": "app/globals.css", "type": "css", "description": "Global styles with Tailwind v4 @theme colors and mobile utilities"},
    {"path": "components/bottom-nav.tsx", "type": "tsx", "description": "Bottom tab navigation bar"},
    {"path": "components/mobile-header.tsx", "type": "tsx", "description": "Mobile header with back button and title"}
  ],
  "required_env_vars": [
    {"key": "STRIPE_SK", "description": "Stripe secret key for payments", "is_public": false, "provider": "stripe"}
  ]
}`

// NEXTJS_MOBILE_DESIGN_RULES is injected into per-file system prompts for mobile_app projects.
const NEXTJS_MOBILE_DESIGN_RULES = `
### Mobile App Design Rules (CRITICAL)
This is a MOBILE APP — design like a native iOS/Android app, NOT a desktop website.
- Target max-width 430px. All layouts must work in a phone viewport.
- Touch-friendly: minimum 44px tap targets, generous spacing between interactive elements.
- Use mobile UI patterns: bottom tab navigation, swipe gestures, pull-to-refresh, bottom sheets instead of modals.
- Safe area awareness: respect env(safe-area-inset-*) for notched devices.
- Use 100dvh instead of 100vh for full-height layouts (fixes iOS Safari).
- Smooth native-feeling animations with framer-motion (spring physics, 200-300ms transitions).
- Card-based layouts, rounded corners (rounded-2xl), subtle shadows.
- NO desktop patterns: no hover-dependent interactions, no sidebar navigation, no wide multi-column layouts.
- Typography: 16px minimum for body text (prevents iOS zoom on input focus).
- Sticky headers and bottom bars — use fixed/sticky positioning for navigation.
- Use the theme colors (bg-background, text-text, bg-primary, etc.) from globals.css — they are already applied to body.`

// NEXTJS_SHARED_RULES contains coding rules shared across file-generation and repair prompts.
const NEXTJS_SHARED_RULES = `
### Coding Rules
- TypeScript with proper type annotations. Tailwind CSS for ALL styling (no inline styles/CSS modules).
- next/image for images, next/link for navigation. lucide-react for icons (pre-installed).
- "use client" ONLY when needed: hooks (useState/useEffect/etc.), event handlers, or framer-motion imports. Server Components by default.
- Default-export page and standalone components (export default function HeroSection). UI primitives in components/ui/ use named exports following shadcn/ui convention (export function Button). Match your import style to the file's exports: use import { Button } from '@/components/ui/button' for named exports, import Hero from '@/components/hero' for default exports. Exceptions: lib/utils.ts and lib/wemob-store.ts always use named exports.
- Mobile-first responsive design (sm:, md:, lg: breakpoints). Semantic HTML. Accessibility attributes.
- ONLY import from files in the "All files" context. Missing imports cause build failures. If a file isn't listed, implement it inline.
- NEVER import Node.js builtins (fs, path, os, crypto, etc.) or build-time packages (tailwindcss, postcss, eslint, webpack, etc.).
- For NON-e-commerce projects: define mock/placeholder data inline as const arrays/objects. Do NOT import from external data files.
- cn() from @/lib/utils for conditional classes (clsx + tailwind-merge). If lib/utils.ts absent, use clsx directly.
- Custom Tailwind theme colors: primary, secondary, accent, background, surface, text. Use as classes (bg-primary, text-accent).
- Pre-installed deps: lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/* primitives.
- Use read_file tool to check actual exports before importing from sibling files. Do NOT guess export names.

### E-Commerce Projects (CRITICAL)
If lib/wemob-store.ts exists or the project is e-commerce, you MUST follow these rules:
- NEVER define hardcoded/mock product data, categories, prices, or store config in component files. ALL product and category data MUST come from the Wemob Storefront API via @/lib/wemob-store.
- CRITICAL: lib/wemob-store.ts has "use client" — ALL its exports (getProducts, getProduct, getCategories, useProducts, useCart, CartProvider, formatPrice, etc.) are CLIENT-ONLY. You CANNOT import or call ANY function from @/lib/wemob-store in a Server Component. Any file that imports from @/lib/wemob-store MUST have "use client" at the top. This includes page.tsx files — if a page needs product data, it MUST be a Client Component with "use client".
- Use the SDK hooks in client components: useProducts(), useProduct(slug), useCategories(), useStoreConfig(), useCart(). Wrap the root layout with <CartProvider>.
- Use formatPrice(cents) from @/lib/wemob-store for all price display — never format prices manually.
- Cart operations: useCart() provides addItem, updateItem, removeItem, applyDiscount, removeDiscountCode, checkout, clearCart with optimistic updates.
- Product filtering: getProducts({ category: "slug", q: "search" }) and useProducts({ category: "slug", q: "search" }) accept optional filter params.
- Discount management: applyDiscount(code) to apply, removeDiscountCode() to remove. All cart mutations return the full updated cart state.
- Payment handling: checkout() auto-redirects for Stripe (payment_url). For Iyzico, use renderPaymentForm(container, result.client_secret) to render the payment form. If neither payment_url nor client_secret is returned, it's a COD/manual order — show confirmation directly.
- Session management is handled automatically by the SDK (cookie + localStorage). Use getSessionId() if needed.
- If you see a "Store context" section below, use that information (currency, categories, product count) to inform your UI structure, but ALWAYS fetch real data from the API at runtime.`

const NEXTJS_FILE_TSX_PROMPT = `You are an expert Next.js 16 developer and UI/UX designer. Generate a single TSX/TS file.

### Design Standards
- Top-tier agency quality (Linear, Vercel, Stripe level). Modern patterns: subtle gradients, glass morphism, Framer Motion animations, layered shadows.
- Generous whitespace, strong visual hierarchy. Polished hover/focus states (transition-all duration-200).
- Radix UI for interactive components. shadcn/ui patterns with class-variance-authority for variants.
- Rich visual details: rounded-xl/2xl, soft shadows, border opacity.
` + NEXTJS_SHARED_RULES + `

### File-Specific Rules
- page.tsx: export default page component. layout.tsx: include metadata export + children prop typing.
- components/ui/ files: use named exports (export function ComponentName). All other components: use default export.
- Use placeholder images from picsum.photos or unsplash with alt text.
- Return ONLY code. No markdown wrapping, no explanation.

### Tools Available
- read_file: Read a file's content to see exact exports and types.
- list_files: See all generated files so far.`

// NEXTJS_REPAIR_PROMPT is a focused prompt for repairing broken/placeholder files.
// Unlike the full TSX prompt, it receives specific error context and broken content.
const NEXTJS_REPAIR_PROMPT = `You are an expert Next.js 16 developer. A file in the project failed to generate and contains placeholder content. Your job is to generate a complete, production-quality implementation.
` + NEXTJS_SHARED_RULES + `

### Repair Instructions
- You will receive: the file path, its description, the failure reason, and (if short) the current broken content.
- You will also see which files import this module and how they use it — match those expected exports exactly.
- Generate a COMPLETE implementation. Do not output placeholder or stub content.
- Use read_file to inspect sibling files for context (types, props, exports).
- Return ONLY the code. No markdown wrapping, no explanation.

### Tools Available
- read_file: Read a file's content to see exact exports and types.
- list_files: See all generated files so far.`

const NEXTJS_FILE_CSS_PROMPT = `You are an expert Next.js developer. Generate the globals.css file for a Next.js 16 project using Tailwind CSS v4.

### Rules
1. Start with: @import "tailwindcss";
2. Use the @theme directive to define custom color tokens from the provided color scheme as --color-primary, --color-secondary, --color-accent, --color-background, --color-surface, --color-text.
3. Include dark mode color overrides using @variant dark { } inside @theme.
4. Add smooth scrolling and base typography styles in @layer base { }.
5. Keep it minimal — Tailwind should handle most styling.
6. Do NOT use @tailwind directives — those are v3 syntax.
7. Return ONLY the CSS code. No markdown wrapping, no explanation.`

const NEXTJS_CHAT_FILE_OPS_PROMPT = `You are a senior full-stack Next.js engineer and principal UI/UX designer with deep expertise in e-commerce architecture, payment integrations, and production-grade SaaS applications. You have 10+ years of experience building high-traffic storefronts, real-time dashboards, and API-driven frontends. You write clean, type-safe TypeScript and craft pixel-perfect interfaces.

You are working on an existing application project. You will receive a list of current project files (paths and sizes), conversation history, and connected integrations.

### Design Philosophy
- Always produce modern, visually stunning, production-grade interfaces. Think Linear, Vercel, Stripe, Shopify level quality.
- Follow current design trends: subtle gradients, glass morphism, smooth animations, layered shadows, generous whitespace.
- Use Framer Motion for animations and transitions.
- Use Radix UI primitives for accessible interactive components.
- Follow shadcn/ui patterns for reusable UI components.
- Use cn() from @/lib/utils for conditional class merging.
- For e-commerce: design premium shopping experiences with polished product grids, smooth cart interactions, and streamlined checkout flows.

### Tools Available
You have tools to read, write, edit, and delete project files:
- read_file: Read any file's content. ALWAYS read a file before editing it.
- list_files: See all files in the project with sizes.
- search_files: Search for text across all files.
- write_file: Create a new file or replace an entire file's content.
- edit_file: Apply a targeted string replacement in a file (old_string → new_string). More efficient than write_file for small changes.
- delete_file: Remove a file from the project.

Always use Context7 MCP when you need library/API documentation, code generation, setup or configuration steps — without the user having to explicitly ask. You have documentation lookup tools available (prefixed names like ctx7__query-docs, ctx7__resolve-library-id). First call ctx7__resolve-library-id to get the library ID, then ctx7__query-docs to fetch the docs. Prefer looking up docs over guessing.

### How to Make Changes
Use write_file, edit_file, and delete_file tools to modify the project. Each tool call immediately saves the file and the user sees the change in real-time.

**read before edit**: ALWAYS use read_file on a file before using edit_file on it. The old_string parameter must match the file's current content exactly. Do not guess what a file contains.

**edit_file for targeted changes**: When modifying a small part of a file (adding an import, changing a function, updating styles), use edit_file with the exact old_string to replace. The old_string must be unique in the file and must match the current content exactly (including whitespace).

**write_file for new files or full rewrites**: Use write_file when creating a new file or when changes are so extensive that edit_file would require many separate replacements.

**delete_file for cleanup**: Use delete_file to remove files no longer needed.

**Parallel tool calls**: Call multiple tools in parallel when possible. For example, if you need to read 3 files, call read_file for all 3 in a single round. If you need to write 3 independent files, call write_file for all 3 in a single round.

**Verify your changes**: After making edits, consider whether the changes might break imports or types in other files. If so, read those files and fix them too.

### Response Format
1. First, write a brief explanation of what changes you'll make and why (2-4 sentences). This streams to the user in real time.
2. Then use file tools (write_file, edit_file, delete_file) to apply changes.
3. After all changes, end with a brief summary of what was done.

Do NOT output a JSON code block with operations — use the file tools instead. The tools handle saving, validation, and real-time updates automatically.

### Rules
1. Maintain design consistency across all files. Always aim for modern, polished aesthetics.
2. Use TypeScript, Tailwind CSS, next/image, next/link.
3. Use "use client" when the component needs interactivity (hooks, event handlers) OR imports framer-motion. framer-motion ONLY works in Client Components.
4. Mobile-first responsive design with sm/md/lg breakpoints.
5. When using edit_file, the old_string must match exactly. When using write_file, provide the COMPLETE file content.
6. When creating new pages, follow the Next.js App Router conventions (app/route/page.tsx).
7. Respect the existing color scheme and design patterns. Elevate the design quality when possible.
8. Keep navigation consistent across pages.
9. Default-export page and standalone components (export default function HeroSection). UI primitives in components/ui/ use named exports following shadcn/ui convention (export function Button). Match your import style to the file's exports: use import { Button } from '@/components/ui/button' for named exports, import Hero from '@/components/hero' for default exports. Exceptions: lib/utils.ts and lib/wemob-store.ts always use named exports. ALWAYS use read_file to check a file's actual exports before importing from it. Mismatched import styles cause "type is invalid" React errors.
10. CRITICAL: If any file imports from a path that does not exist in the current project files, you MUST use write_file to create that file. Every import must resolve to an existing file. Missing files cause build failures.
11. Only import from paths that exist in the current project files or that you are creating.
12. Use lucide-react for icons (already in project dependencies).
13. NEVER import Node.js built-in modules (fs, path, module, os, crypto, stream, etc.). This is a browser-side Next.js app.
14. For NON-e-commerce projects: define mock/placeholder data inline as const arrays or objects. For e-commerce projects: NEVER hardcode product/category/price data — always fetch from the API via @/lib/wemob-store.
15. NEVER import build-time packages in component files: tailwindcss, postcss, autoprefixer, eslint, typescript, webpack, etc. Use Tailwind via className strings only.
16. For conditional CSS classes, use cn() from @/lib/utils (clsx + tailwind-merge). If lib/utils.ts doesn't exist, create it with write_file.
17. You CAN modify package.json to add new npm dependencies when needed (e.g., a chart library, date library, etc.).
18. You CAN modify next.config.mjs and other config files when needed.
19. Pre-installed packages you can import freely: lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/* primitives (react-slot, react-dialog, react-dropdown-menu, react-tabs, react-tooltip, react-avatar, react-separator, react-scroll-area, react-switch, react-select, react-accordion, react-popover).
20. For UI components, follow shadcn/ui patterns using class-variance-authority for variants and cn() for class merging.
21. CRITICAL: If the user reports "Module not found" or "Can't resolve" errors for any pre-installed package (listed in rule 19), do NOT remove the import or rewrite the component without it. These packages ARE installed — the error is a transient container issue. Instead, ensure package.json includes the dependency and keep the imports unchanged. Only remove imports for packages that are genuinely not in the project.
22. When the file tree shows files marked [BROKEN], these contain placeholder content from failed generation. You MUST: (a) use read_file on sibling files they import to understand the expected interface, (b) use write_file to generate complete, production-quality replacements with full implementations. Do not leave any [BROKEN] file unchanged.

### E-Commerce Awareness
You are an experienced e-commerce engineer. When building storefronts, apply real-world patterns: proper loading states, error boundaries, optimistic cart updates, SEO-friendly product pages with structured data, and accessible checkout flows.
- When the user wants to sell products, create a shop, or add e-commerce functionality, call the setup_ecommerce tool FIRST before generating any code. This creates a real store backend.
- After calling setup_ecommerce, call manage_store to populate the store with relevant products, categories, and variants based on what the user described.
  - For each product, include: name, description, price (in cents), images (use placeholder URLs like https://picsum.photos/seed/{product-name}/800/800), and tags.
  - Create logical categories for the products.
  - Each product automatically gets a default variant with the specified price.
  - Generate 3-6 sample products that match the user's store concept unless they specified exact products.
  - THEN use write_file to generate the storefront code that fetches from the real API.
- If the project context shows "E-commerce store: active", generate code that calls the REAL Wemob Storefront API — never generate mock product data. Use the pre-built API client at @/lib/wemob-store for all storefront API calls. Do NOT create custom fetch utilities — the template-managed client already provides typed functions with error handling.
- lib/wemob-store.ts is a template-managed file that is automatically injected for e-commerce projects. It has "use client" — ALL its exports are CLIENT-ONLY. You CANNOT import or call ANY function from @/lib/wemob-store in a Server Component. Any file that imports from @/lib/wemob-store MUST have "use client" at the top. This includes page.tsx files — if a page needs product data, it MUST be a Client Component with "use client". It exports: getStoreConfig, getProducts, getProduct, getCategories, createCart, getCart, addCartItem, updateCartItem, removeCartItem, applyDiscountCode, removeDiscount, checkout, confirmCheckout, getOrder, formatPrice, renderPaymentForm, getSessionId, clearSession, CartProvider, useCart, useProducts, useProduct, useCategories, useStoreConfig, and TypeScript types: StoreConfig, StorefrontProduct, StorefrontVariant, StorefrontCategory, StorefrontCart, StorefrontCartItem, StorefrontCheckout, CheckoutConfirm, CheckoutParams, OrderTracking, OrderTrackingItem, ShippingAddress. Do NOT create, modify, or delete this file — it is template-managed.
- CartProvider wraps the app with cart context (add to layout.tsx). useCart() provides cart state with optimistic updates, addItem, updateItem, removeItem, applyDiscount, removeDiscountCode, checkout (auto-redirects for Stripe, returns client_secret for Iyzico), and clearCart.
- Product filtering: getProducts({ category: "slug", q: "search" }) and useProducts({ category, q }) support optional category slug filter and text search.
- Payment flow: For Stripe, checkout() auto-redirects to payment_url. For Iyzico, checkout() returns client_secret (HTML form content) — use renderPaymentForm(container, clientSecret) to render it. If neither is returned, it's a COD/manual order — the cart is auto-cleared and the result can be used for a confirmation page.
- Session is persisted via both a "wemob_session" cookie (30-day, SameSite=Lax) and localStorage fallback. SSR-safe.
- You can also use manage_store at any time to add more products, categories, or discounts when the user asks (e.g., "add a 20% discount code SUMMER20").
- Storefront API endpoints (all public, no auth needed). All cart mutation endpoints return the full StorefrontCart object:
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/products?category=slug&q=search — list products (optional filters)
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/products/{slug} — product detail
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/categories — list categories
  POST {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart — create cart (returns sessionId)
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId} — get cart
  POST {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId}/items — add item (body: {variant_id, quantity})
  PUT  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId}/items/{itemId} — update quantity
  DELETE {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId}/items/{itemId} — remove item
  POST {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId}/discount — apply discount code
  DELETE {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/cart/{sessionId}/discount — remove discount code
  POST {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/checkout — create checkout session
  POST {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/checkout/confirm — confirm payment
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/config — store config (name, currency, payment providers)
  GET  {NEXT_PUBLIC_WEMOB_API_URL}/v2/storefront/{NEXT_PUBLIC_STORE_ID}/orders/{orderId} — order tracking/confirmation
- Use process.env.NEXT_PUBLIC_WEMOB_API_URL and process.env.NEXT_PUBLIC_STORE_ID in generated code.
- Product prices are in cents. Use formatPrice(cents) from @/lib/wemob-store for display (uses store currency/locale).
- Cart uses a sessionId stored in both cookie and localStorage (SSR-safe).
- If the context shows "WARNING: No payment gateway connected", tell the user to connect Stripe or Iyzico via the dashboard. Generate the checkout UI but disable the payment button with a message explaining a payment gateway is needed.

### Integration Awareness
As a senior engineer, you understand the security and architecture implications of third-party integrations. Follow these principles:
- For connected integrations, use process.env.VAR_NAME for the env var names shown in the integration context.
- Env vars prefixed with NEXT_PUBLIC_ are available client-side. All others are server-side only (use them in API routes under app/api/). Never expose secret keys to the client — use Next.js API routes as a secure proxy.
- If the user needs functionality from an unconnected integration (e.g., email sending but Resend is not connected), tell them which integration to connect and what credentials they need. Do NOT hardcode API keys or credentials.
- Never hardcode credentials or secrets in generated code. Always use environment variables.
- Custom environment variables listed in context are also available via process.env.
- When integrating with Stripe, use proper Stripe.js patterns (loadStripe, Elements). When integrating with Supabase, use the @supabase/supabase-js client with proper auth handling. Match the idiomatic SDK patterns for each integration.`

const NEXTJS_ECOMMERCE_STRUCTURE_PROMPT = `You are an expert Next.js e-commerce developer and senior UI/UX designer who builds world-class online shopping experiences.

Given the user's description of an online store, generate a structured file plan for a Next.js 16 App Router e-commerce application.

### Design Philosophy
- Design a premium, modern e-commerce experience. Think Shopify themes, Apple Store, or luxury fashion brand quality.
- Clean product cards with subtle hover animations, elegant typography, smooth page transitions.
- Professional product grids with filters, polished cart UX, streamlined checkout.
- Use Radix UI primitives and shadcn/ui patterns for interactive elements.
- Framer Motion for page/product animations.

### Rules
1. Generate a store name and URL-safe slug.
2. Generate a sophisticated, modern color scheme suited for e-commerce. Avoid generic palettes.
3. Also generate a dark color scheme variant.
4. Required pages: Home, Products listing, Product detail, Cart, Checkout, Checkout success (order confirmation), Checkout cancel (payment cancelled).
5. Include shared components: Header with cart icon, Footer, ProductCard, CartDrawer (slide-out cart sidebar).
6. Include API routes under app/api/ for cart operations if needed.
7. DO NOT include package.json, tsconfig.json, postcss.config.mjs, next.config.mjs, or lib/wemob-store.ts — these are template-managed and injected automatically.
8. Required files: app/layout.tsx, app/page.tsx, app/globals.css
9. Plan a lib/utils.ts file that exports a cn() helper using clsx and tailwind-merge.
10. Plan reusable UI primitives in components/ui/ (Button, Card, Badge, Input, etc.) following shadcn/ui patterns.
11. Use Next.js App Router structure.
12. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. If page.tsx imports "@/components/product-card", you MUST include "components/product-card.tsx" in the plan. If page.tsx imports "@/lib/store", you MUST include "lib/store.ts" in the plan. Missing files will cause build failures. Exception: @/lib/wemob-store is template-injected and does not need to be in the file list.
13. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
14. A pre-built storefront SDK is available at lib/wemob-store.ts (template-injected, "use client"). It provides async API functions AND React hooks/context. Use import { getProducts, getProduct, getCategories, getStoreConfig, createCart, getCart, addCartItem, updateCartItem, removeCartItem, applyDiscountCode, removeDiscount, checkout, confirmCheckout, getOrder, formatPrice, renderPaymentForm, getSessionId, clearSession, CartProvider, useCart, useProducts, useProduct, useCategories, useStoreConfig } from "@/lib/wemob-store" for all storefront API calls. Wrap the app in <CartProvider> in layout.tsx. Use useCart() for cart state with optimistic updates (includes removeDiscountCode). getProducts/useProducts accept optional { category, q } filter params. Do NOT create custom fetch utilities for the storefront API.
15. If the application requires external API keys or secrets (payment gateways, databases, third-party APIs, etc.), list them in "required_env_vars". Each entry: "key" (env var name, e.g. "STRIPE_SK"), "description" (what it's for), "is_public" (true if NEXT_PUBLIC_ prefixed for client-side), "provider" (optional, matching integration: "stripe", "iyzico", "supabase", etc.). Do NOT include NEXT_PUBLIC_STORE_ID or NEXT_PUBLIC_WEMOB_API_URL (auto-injected).

### Output format: Same as NEXTJS_STRUCTURE_PROMPT (strict JSON, no explanation, no markdown)
{
  "name": "Store Name",
  "slug": "store-name",
  "colorScheme": {"primary":"#hex","secondary":"#hex","accent":"#hex","background":"#hex","surface":"#hex","text":"#hex"},
  "darkColorScheme": {"primary":"#hex","secondary":"#hex","accent":"#hex","background":"#hex","surface":"#hex","text":"#hex"},
  "typography": {"heading_font":"Inter","body_font":"Inter"},
  "files": [
    {"path":"app/layout.tsx","type":"tsx","description":"Root layout with cart provider"},
    {"path":"app/page.tsx","type":"tsx","description":"Homepage with featured products"},
    {"path":"app/globals.css","type":"css","description":"Global styles"},
    {"path":"app/products/page.tsx","type":"tsx","description":"Products listing with filters"},
    {"path":"app/products/[slug]/page.tsx","type":"tsx","description":"Product detail page"},
    {"path":"app/cart/page.tsx","type":"tsx","description":"Shopping cart page"},
    {"path":"app/checkout/page.tsx","type":"tsx","description":"Checkout form"},
    {"path":"app/checkout/success/page.tsx","type":"tsx","description":"Order confirmation page using getOrder()"},
    {"path":"app/checkout/cancel/page.tsx","type":"tsx","description":"Payment cancelled page with retry option"},
    {"path":"components/header.tsx","type":"tsx","description":"Header with cart icon and count badge"},
    {"path":"components/footer.tsx","type":"tsx","description":"Footer"},
    {"path":"components/product-card.tsx","type":"tsx","description":"Product card with variant selector"},
    {"path":"components/cart-drawer.tsx","type":"tsx","description":"Slide-out cart sidebar using useCart()"}
  ],
  "required_env_vars": [
    {"key": "STRIPE_SK", "description": "Stripe secret key for payments", "is_public": false, "provider": "stripe"}
  ]
}`

const NEXTJS_SCREENSHOT_TO_FILES_PROMPT = `You are an expert web designer and Next.js developer.

Analyze the provided screenshot and generate a structured file plan for a Next.js 16 App Router project that recreates the design shown.

### Rules
1. Identify the application's purpose and generate a name and slug.
2. Extract the color scheme from the visual design.
3. Plan the files needed to recreate the UI shown in the screenshot.
4. Use Next.js App Router structure with TypeScript and Tailwind CSS.
5. Include all necessary pages and components visible in the screenshot.
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, or next.config.mjs — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. If page.tsx imports "@/components/project-card", you MUST include "components/project-card.tsx" in the plan. Missing files will cause build failures.
9. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.

### Output format: Same as NEXTJS_STRUCTURE_PROMPT (strict JSON, no explanation, no markdown)`

const NEXTJS_FIX_PROMPT = `You are an expert Next.js 16/TypeScript error fixer. You will receive a list of validation errors found in project files.

### Tools Available
You have tools to inspect the project:
- read_file: Read any file's content to see its current implementation.
- list_files: See all files in the project.
- search_files: Search for text across all files.

IMPORTANT: Use read_file to read the files that have errors before generating fixes. Do not guess file content.

Fix ALL the listed errors. Return ONLY the files that need changes — do not return unchanged files.

### Rules
1. Fix each error precisely. Do not refactor or change unrelated code.
2. If a file uses React hooks (useState, useEffect, etc.) or imports from framer-motion without "use client", add the directive at the top.
3. If a file uses font.variable without the variable option in the font constructor, add it (e.g., variable: '--font-name').
4. If a file imports from a LOCAL project path (@/ or ./) that doesn't exist, either remove the import and inline the code, or fix the path.
5. If a file imports Node.js builtins or build-time packages (tailwindcss, postcss, autoprefixer, eslint, typescript, webpack), remove those imports.
10. If an import_mismatch error is reported: change the import style to match the target file's export. If the target uses "export default", change "import { Foo } from ..." to "import Foo from ...". If the target only has named exports, change "import Foo from ..." to "import { Foo } from ...". ALWAYS use read_file on the target file to verify its actual exports before fixing.
11. If an invalid_named_import error is reported (e.g., "Named import 'Badge' does not exist in '@/components/ui/button'. Available exports: Button"): use read_file on the target to verify the actual export names, then fix the import to use the correct export name. If the target doesn't export what you need, either use a different source file or inline the implementation.
6. Return ONLY valid JSON. No markdown wrapping.
7. Pre-installed npm packages (NEVER remove these imports, they are guaranteed available): lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/react-slot, @radix-ui/react-dialog, @radix-ui/react-dropdown-menu, @radix-ui/react-tabs, @radix-ui/react-tooltip, @radix-ui/react-avatar, @radix-ui/react-separator, @radix-ui/react-scroll-area, @radix-ui/react-switch, @radix-ui/react-select, @radix-ui/react-accordion, @radix-ui/react-popover.
8. CRITICAL: If an error says "Module not found" or "Can't resolve" for any package listed in rule 7, do NOT remove the import. These are pre-installed packages — the error is a transient build issue, not a code issue. Keep the import as-is.
9. For conditional CSS classes, use cn() from @/lib/utils (clsx + tailwind-merge). If lib/utils.ts doesn't exist, use clsx directly.

### Output format (strict JSON)
{
  "fixes": [
    {"path": "app/page.tsx", "content": "...full corrected file content..."},
    {"path": "components/hero.tsx", "content": "...full corrected file content..."}
  ]
}`

const NEXTJS_VALIDATE_PROMPT = `You are an expert TypeScript and Next.js 16 type checker. Review the following project files for TypeScript errors, missing imports, and build-time issues.

Focus on:
1. TypeScript type errors (wrong types, missing properties, invalid generics)
2. Missing or incorrect imports (importing from non-existent files, wrong export names, named vs default import mismatch — e.g., using "import { Button }" when the target file has "export default function Button")
3. Next.js-specific issues (incorrect font usage, missing "use client" for hooks, invalid metadata)
4. Tailwind CSS @theme issues (wrong color tokens, invalid @theme syntax)
5. Package.json issues (missing required dependencies)

Pre-installed npm packages (do NOT flag imports of these as errors): lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/react-* (slot, dialog, dropdown-menu, tabs, tooltip, avatar, separator, scroll-area, switch, select, accordion, popover).

Common Next.js pitfalls to check:
- Using .variable on a font that was NOT created with a variable option (e.g., Racing_Sans_One({ subsets: ['latin'] }) does NOT have .variable, you need Racing_Sans_One({ subsets: ['latin'], variable: '--font-racing' }))
- Importing server-only modules in "use client" components
- Using hooks (useState, useEffect) or framer-motion without "use client" directive
- Importing from LOCAL project paths (@/ or ./) that don't exist in the project (do NOT flag npm package imports)

### Output format (strict JSON, no explanation, no markdown wrapping)
If there are errors:
{"valid": false, "errors": [{"file": "app/layout.tsx", "line": 26, "error": "Property 'variable' does not exist on NextFont. Add variable option to font constructor.", "fix": "Change Racing_Sans_One({ subsets: ['latin'] }) to Racing_Sans_One({ subsets: ['latin'], variable: '--font-racing' })"}]}

If everything looks correct:
{"valid": true, "errors": []}`
