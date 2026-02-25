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

// NEXTJS_DB_TEMPLATE_FILE_PATHS are template-managed files injected only after successful Neon provisioning.
var NEXTJS_DB_TEMPLATE_FILE_PATHS = []string{
	"lib/db.ts",
	"lib/schema.ts",
}

// NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS are template-managed files injected only for e-commerce projects.
var NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS = []string{
	"lib/wemob-store.ts",
}

// NEXTJS_UI_TEMPLATE_FILE_PATHS are pre-built UI component templates injected during initial generation
// so the LLM doesn't waste tokens regenerating them from scratch.
var NEXTJS_UI_TEMPLATE_FILE_PATHS = []string{
	"lib/utils.ts",
	"components/ui/button.tsx",
	"components/ui/card.tsx",
	"components/ui/input.tsx",
	"components/ui/badge.tsx",
	"components/ui/skeleton.tsx",
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
    "@radix-ui/react-popover": "^1.1.0",
    "drizzle-orm": "^0.39.0",
    "@neondatabase/serverless": "^0.10.0"
  },
  "devDependencies": {
    "typescript": "^5.4.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@types/node": "^20.0.0",
    "tailwindcss": "^4.0.0",
    "@tailwindcss/postcss": "^4.0.0",
    "postcss": "^8.4.0",
    "drizzle-kit": "^0.30.0"
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

// NEXTJS_TEMPLATE_DB_TS is the database connection file using Neon serverless + Drizzle ORM.
// The %s placeholder is replaced with the actual Neon connection string at codegen time.
const NEXTJS_TEMPLATE_DB_TS = `import { neon } from '@neondatabase/serverless';
import { drizzle } from 'drizzle-orm/neon-http';
import * as schema from './schema';

const sql = neon(process.env.DATABASE_URL || '%s');
export const db = drizzle(sql, { schema });
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
        clearCartCache();
      }

      return result;
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

// --- Pre-built UI component templates (shadcn/ui patterns) ---

const NEXTJS_TEMPLATE_UTILS_TS = `import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
`

const NEXTJS_TEMPLATE_BUTTON_TSX = `import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90",
        outline: "border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground",
        secondary: "bg-secondary text-secondary-foreground shadow-sm hover:bg-secondary/80",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 rounded-md px-3 text-xs",
        lg: "h-10 rounded-md px-8",
        icon: "h-9 w-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
`

const NEXTJS_TEMPLATE_CARD_TSX = `import * as React from "react";
import { cn } from "@/lib/utils";

const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("rounded-xl border bg-card text-card-foreground shadow", className)} {...props} />
  )
);
Card.displayName = "Card";

const CardHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col space-y-1.5 p-6", className)} {...props} />
  )
);
CardHeader.displayName = "CardHeader";

const CardTitle = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("font-semibold leading-none tracking-tight", className)} {...props} />
  )
);
CardTitle.displayName = "CardTitle";

const CardDescription = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
  )
);
CardDescription.displayName = "CardDescription";

const CardContent = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("p-6 pt-0", className)} {...props} />
  )
);
CardContent.displayName = "CardContent";

const CardFooter = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex items-center p-6 pt-0", className)} {...props} />
  )
);
CardFooter.displayName = "CardFooter";

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter };
`

const NEXTJS_TEMPLATE_INPUT_TSX = `import * as React from "react";
import { cn } from "@/lib/utils";

const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        className={cn(
          "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
          className
        )}
        ref={ref}
        {...props}
      />
    );
  }
);
Input.displayName = "Input";

export { Input };
`

const NEXTJS_TEMPLATE_BADGE_TSX = `import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground shadow hover:bg-primary/80",
        secondary: "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive: "border-transparent bg-destructive text-destructive-foreground shadow hover:bg-destructive/80",
        outline: "text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
`

const NEXTJS_TEMPLATE_SKELETON_TSX = `import { cn } from "@/lib/utils";

function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-primary/10", className)} {...props} />;
}

export { Skeleton };
`

// NEXTJS_BUILDER_SYSTEM_PROMPT is the single unified system prompt used for both
// initial app generation and chat editing. It replaces all previous per-phase prompts.
// Block 1 (stable, cacheable): role, tools, framework rules, design philosophy.
const NEXTJS_BUILDER_SYSTEM_PROMPT = `You are a senior full-stack Next.js engineer and principal UI/UX designer with deep expertise in e-commerce architecture, payment integrations, and production-grade SaaS applications. You have 10+ years of experience building high-traffic storefronts, real-time dashboards, and API-driven frontends. You write clean, type-safe TypeScript and craft pixel-perfect interfaces.

You build and edit web applications using Next.js 16 App Router, TypeScript, and Tailwind CSS v4.

### Design Philosophy
- Always produce modern, visually stunning, production-grade interfaces. Think Linear, Vercel, Stripe, Shopify level quality.
- Follow current design trends: subtle gradients, glass morphism, smooth animations, layered shadows, generous whitespace.
- Use Framer Motion for animations and transitions.
- Use Radix UI primitives for accessible interactive components.
- Follow shadcn/ui patterns for reusable UI components.
- Use cn() from @/lib/utils for conditional class merging (clsx + tailwind-merge).
- For e-commerce: design premium shopping experiences with polished product grids, smooth cart interactions, and streamlined checkout flows.

### Tools Available
You have tools to read, write, edit, and delete project files:
- read_file: Read any file's content. ALWAYS read a file before editing it.
- list_files: See all files in the project with sizes.
- search_files: Search for text across all files.
- write_file: Create a new file or replace an entire file's content.
- edit_file: Apply a targeted string replacement in a file (old_string → new_string). More efficient than write_file for small changes.
- delete_file: Remove a file from the project.
- setup_ecommerce: Create an e-commerce store backend when the app needs product/cart/checkout features.
- manage_store: Add products, categories, variants, and discounts to an existing store.
- setup_database: Provision a PostgreSQL database with schema tables when the app needs persistent data storage.

### How to Build a New App (Initial Generation)
When building a new app from a user's prompt:
1. **Think architecture first**: Decide the page structure, components, data models, and routing.
2. **Skip template-managed files**: lib/utils.ts and components/ui/ primitives (button, card, input, badge, skeleton) are already injected. Do NOT recreate them. Import directly: import { Button } from '@/components/ui/button'.
3. **Write feature components**: Build the reusable feature-specific components (Header, Footer, ProductCard, etc.).
4. **Write pages**: Build each page using the components you just created.
5. **Write app/globals.css**: Design a complete, cohesive globals.css with @import "tailwindcss" and @theme directive for custom colors. Choose a sophisticated, modern color palette that fits the app's purpose. Include dark mode support via @media (prefers-color-scheme: dark).
6. **Write app/layout.tsx LAST**: It must include <html lang="en"> and <body> tags wrapping {children}. Import Google Fonts using next/font/google. Export metadata.
7. **Every file you create must be complete**: No placeholders, no "TODO", no stub implementations.
8. **Use write_files to batch-create files**: Group related files into a single write_files call (e.g., all UI primitives in one call, all pages in another). Each file is saved and shown to the user in real-time. Do NOT re-write files that already succeeded.

### How to Edit an Existing App (Chat)
When the user asks for changes to an existing app:
1. Use list_files to understand the current project structure.
2. Use read_file to see the current implementation of files you need to modify.
3. Use edit_file for targeted changes (adding imports, modifying functions, updating styles).
4. Use write_file when creating new files or doing extensive rewrites.
5. After making changes, consider whether imports or types in other files might break. Fix them too.
6. Verify your changes maintain visual consistency and don't break navigation.

### Response Format
1. First, write a brief explanation of what you'll do (2-4 sentences). This streams to the user in real time.
2. Then use file tools (write_file, edit_file, delete_file) to apply changes.
3. After all changes, end with a brief summary of what was done.

### Framework Rules
- TypeScript with proper type annotations. Tailwind CSS for ALL styling (no inline styles/CSS modules).
- next/image for images, next/link for navigation. lucide-react for icons (pre-installed).
- "use client" ONLY when needed: hooks (useState/useEffect/etc.), event handlers, or framer-motion imports. Server Components by default.
- Default-export page and standalone components (export default function HeroSection). UI primitives in components/ui/ use named exports following shadcn/ui convention (export function Button). Match your import style to the file's exports: use import { Button } from '@/components/ui/button' for named exports, import Hero from '@/components/hero' for default exports. Exceptions: lib/utils.ts and lib/wemob-store.ts always use named exports.
- Mobile-first responsive design (sm:, md:, lg: breakpoints). Semantic HTML. Accessibility attributes.
- ONLY import from files that exist in the project or that you are creating. Missing imports cause build failures.
- ONLY create <Link href="..."> navigation to pages that exist or that you are creating. No dead links.
- NEVER import Node.js builtins (fs, path, os, crypto, etc.) or build-time packages (tailwindcss, postcss, eslint, webpack, etc.).
- Use ` + "`" + `import { db } from '@/lib/db'` + "`" + ` and ` + "`" + `import { ... } from '@/lib/schema'` + "`" + ` for all data access. Use Drizzle queries in server components and API routes. Do NOT use hardcoded mock data — all data should come from the database. Import Drizzle operators from 'drizzle-orm': ` + "`" + `import { eq, and, desc, sql } from 'drizzle-orm'` + "`" + `.
- cn() from @/lib/utils for conditional classes (clsx + tailwind-merge). lib/utils.ts is template-managed — do NOT recreate it.
- lib/utils.ts and components/ui/ primitives (button, card, input, badge, skeleton) are template-managed. Do NOT recreate or modify them. Import directly: import { Button } from '@/components/ui/button'.
- Custom Tailwind theme colors: primary, secondary, accent, background, surface, text. Use as classes (bg-primary, text-accent).
- Pre-installed deps: lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/* primitives (react-slot, react-dialog, react-dropdown-menu, react-tabs, react-tooltip, react-avatar, react-separator, react-scroll-area, react-switch, react-select, react-accordion, react-popover).
- Use read_file tool to check actual exports before importing from sibling files. Do NOT guess export names.
- ALWAYS use read_file on a file before using edit_file on it. The old_string parameter must match the file's current content exactly.
- You CAN modify package.json to add new npm dependencies when needed.
- CRITICAL: app/layout.tsx (root layout) MUST include <html lang="en"> and <body> tags wrapping {children}. This is a Next.js App Router requirement — builds will fail without them.
- Use placeholder images from picsum.photos with descriptive alt text for any images.
- Parallel tool calls: Call multiple read_file or search_files tools in parallel when possible.
- CRITICAL: If the user reports "Module not found" errors for pre-installed packages, keep the imports — these are transient container issues. Ensure package.json includes the dependency.

### Integration Rules
- For connected integrations, use process.env.VAR_NAME for env var names shown in context.
- Env vars prefixed with NEXT_PUBLIC_ are available client-side. All others are server-side only.
- Never hardcode credentials or secrets. Always use environment variables.
- When integrating with Stripe, use proper Stripe.js patterns. Match idiomatic SDK patterns for each integration.`

// NEXTJS_BUILDER_ECOMMERCE_RULES is appended to the system prompt only for e-commerce projects.
const NEXTJS_BUILDER_ECOMMERCE_RULES = `

### E-Commerce Rules
- Call setup_ecommerce FIRST before generating storefront code. Then call manage_store to populate products.
- NEVER define hardcoded/mock product data. ALL data comes from the Wemob Storefront API via @/lib/wemob-store.
- lib/wemob-store.ts is template-managed and "use client" — ALL its exports are CLIENT-ONLY. Any file importing from it MUST have "use client".
- It exports: getStoreConfig, getProducts, getProduct, getCategories, createCart, getCart, addCartItem, updateCartItem, removeCartItem, applyDiscountCode, removeDiscount, checkout, confirmCheckout, getOrder, formatPrice, renderPaymentForm, getSessionId, clearSession, CartProvider, useCart, useProducts, useProduct, useCategories, useStoreConfig, plus TypeScript types.
- CartProvider wraps the app (add to layout.tsx). useCart() provides cart state with optimistic updates.
- Product prices are in cents. Use formatPrice(cents) for display.
- Do NOT create, modify, or delete lib/wemob-store.ts — it is template-managed.`

// NEXTJS_BUILDER_AUTH_RULES is appended to the system prompt only when the project uses authentication.
const NEXTJS_BUILDER_AUTH_RULES = `

### Auth Rules
- Server-side: ` + "`" + `import { register, login, logout, getSession, getCurrentUser, requireAuth, getApiSession } from '@/lib/wemob-auth'` + "`" + `. Use requireAuth() in server components for protected pages. Use getApiSession() in API routes.
- Client-side: ` + "`" + `import { AuthProvider, useAuth } from '@/lib/wemob-auth-client'` + "`" + `. Wrap root layout with <AuthProvider>. useAuth() returns { user, isLoading, isAuthenticated, login, register, logout, refreshSession }.
- NEVER create custom auth utilities. Use the injected auth libraries.
- lib/wemob-auth.ts is SERVER-ONLY. NEVER import it in "use client" files. Use lib/wemob-auth-client.tsx for client components.
- Create an API route at app/api/auth/[...action]/route.ts that dispatches to register/login/logout/getSession from lib/wemob-auth.`

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
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, next.config.mjs, lib/db.ts, lib/schema.ts, lib/utils.ts, components/ui/button.tsx, components/ui/card.tsx, components/ui/input.tsx, components/ui/badge.tsx, or components/ui/skeleton.tsx — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. Use Next.js App Router structure (app/ directory for pages).
9. Plan 3-8 pages depending on complexity.
10. Include shared UI components in a components/ directory. You may plan additional components/ui/ primitives beyond the pre-injected ones (button, card, input, badge, skeleton).
11. Keep the structure clean and well-organized.
12. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. If page.tsx imports "@/components/project-card", you MUST include "components/project-card.tsx" in the plan. If page.tsx imports "@/lib/projects", you MUST include "lib/projects.ts" in the plan. Missing files will cause build failures.
14. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
15. If the application requires external API keys or secrets (payment gateways, databases, third-party APIs, etc.), list them in "required_env_vars". Each entry: "key" (env var name, e.g. "STRIPE_SK"), "description" (what it's for), "is_public" (true if NEXT_PUBLIC_ prefixed for client-side), "provider" (optional, matching integration: "stripe", "iyzico", "supabase", etc.). Do NOT include NEXT_PUBLIC_STORE_ID, NEXT_PUBLIC_WEMOB_API_URL, or DATABASE_URL (auto-provisioned).
16. Design "data_models" — an array of database tables the app needs for its data. Each table has a "name" and "columns" array. Each column has "name", "type" (text, integer, boolean, timestamp, uuid, jsonb), "primary_key" (bool), "nullable" (bool), optional "default", and optional "references" (e.g. "categories.id"). Every table MUST have an "id" column with type "uuid" and primary_key true. Add "created_at" and "updated_at" timestamp columns. Design the schema based on what the app actually needs — blogs need posts/comments, task apps need tasks/projects, etc. A database will be provisioned automatically from these models.
17. Plan app/api/ routes for CRUD operations using Drizzle ORM. Each API route should handle GET/POST/PUT/DELETE for its resource.
18. Do NOT use hardcoded mock data. All data must come from the database via Drizzle queries. Show graceful empty states when no data exists yet.
19. If the application needs user authentication (login, register, user accounts, protected pages, admin panels, dashboards, or user-specific data), set "needs_auth": true. Auth infrastructure (lib/wemob-auth.ts and lib/wemob-auth-client.tsx) will be auto-injected. Plan login/register pages and an API route at app/api/auth/[...action]/route.ts to dispatch auth actions. Do NOT plan custom auth utilities — they are template-managed.

### Output format (strict JSON, no explanation, no markdown)
{
  "name": "App Name",
  "slug": "app-name",
  "needs_auth": false,
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
  "data_models": [
    {
      "name": "posts",
      "columns": [
        {"name": "id", "type": "uuid", "primary_key": true},
        {"name": "title", "type": "text", "primary_key": false, "nullable": false},
        {"name": "content", "type": "text", "primary_key": false, "nullable": false},
        {"name": "created_at", "type": "timestamp", "primary_key": false, "nullable": false, "default": "now()"},
        {"name": "updated_at", "type": "timestamp", "primary_key": false, "nullable": false, "default": "now()"}
      ]
    }
  ],
  "files": [
    {"path": "app/layout.tsx", "type": "tsx", "description": "Root layout with fonts, metadata, and navigation"},
    {"path": "app/page.tsx", "type": "tsx", "description": "Homepage with hero section"},
    {"path": "app/globals.css", "type": "css", "description": "Global styles with Tailwind v4 @theme colors"},
    {"path": "app/api/posts/route.ts", "type": "ts", "description": "API route for posts CRUD"},
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
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, next.config.mjs, lib/db.ts, lib/schema.ts, lib/utils.ts, components/ui/button.tsx, components/ui/card.tsx, components/ui/input.tsx, components/ui/badge.tsx, or components/ui/skeleton.tsx — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. Use Next.js App Router structure (app/ directory for pages).
9. Plan 3-8 pages depending on complexity.
10. Include shared UI components in a components/ directory. You may plan additional components/ui/ primitives beyond the pre-injected ones (button, card, input, badge, skeleton).
11. Keep the structure clean and well-organized.
12. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. Missing files will cause build failures.
14. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
15. MOBILE-SPECIFIC: Include viewport meta tag configuration for mobile. Use touch-friendly sizing. Plan bottom tab navigation component. Use mobile-appropriate components (bottom sheets, slide-up modals, swipeable cards). All layouts must work within 430px max width. NO desktop-only layouts.
16. Generate mobile-first CSS — use Tailwind's default breakpoint as the base (mobile), never require larger breakpoints for core functionality.
17. Include PWA-ready manifest hints in the layout description.
18. If the application requires external API keys or secrets (payment gateways, databases, third-party APIs, etc.), list them in "required_env_vars". Each entry: "key" (env var name, e.g. "STRIPE_SK"), "description" (what it's for), "is_public" (true if NEXT_PUBLIC_ prefixed for client-side), "provider" (optional, matching integration: "stripe", "iyzico", "supabase", etc.). Do NOT include NEXT_PUBLIC_STORE_ID or NEXT_PUBLIC_WEMOB_API_URL (auto-injected).
19. If the application needs user authentication (login, register, user accounts, protected pages, admin panels, dashboards, or user-specific data), set "needs_auth": true. Auth infrastructure (lib/wemob-auth.ts and lib/wemob-auth-client.tsx) will be auto-injected. Plan login/register pages and an API route at app/api/auth/[...action]/route.ts to dispatch auth actions. Do NOT plan custom auth utilities — they are template-managed.

### Output format (strict JSON, no explanation, no markdown)
{
  "name": "App Name",
  "slug": "app-name",
  "needs_auth": false,
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
6. Include API routes under app/api/ for cart operations, checkout, products, etc.
7. DO NOT include package.json, tsconfig.json, postcss.config.mjs, next.config.mjs, lib/db.ts, or lib/schema.ts — these are template-managed and injected automatically.
8. Required files: app/layout.tsx, app/page.tsx, app/globals.css
9. Plan a lib/utils.ts file that exports a cn() helper using clsx and tailwind-merge.
10. Plan reusable UI primitives in components/ui/ (Button, Card, Badge, Input, etc.) following shadcn/ui patterns.
11. Use Next.js App Router structure.
12. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. Missing files will cause build failures.
13. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.
14. The e-commerce schema (products, categories, carts, orders, etc.) is pre-defined and injected automatically. You may add additional data_models tables if the store concept needs them (e.g., reviews, wishlists), but do NOT redefine the core ecommerce tables.
15. All data access uses Drizzle ORM: import { db } from '@/lib/db' and import { products, categories, ... } from '@/lib/schema'. Use db.select().from(products) for queries. Server-side data fetching by default. A database is provisioned automatically.
16. If the application requires external API keys or secrets, list them in "required_env_vars". Do NOT include DATABASE_URL (auto-provisioned).
17. If the application needs user authentication (login, register, user accounts, protected pages, admin panels, dashboards, or user-specific data), set "needs_auth": true. Auth infrastructure (lib/wemob-auth.ts and lib/wemob-auth-client.tsx) will be auto-injected. Plan login/register pages and an API route at app/api/auth/[...action]/route.ts to dispatch auth actions. Do NOT plan custom auth utilities — they are template-managed.

### Output format: Same as NEXTJS_STRUCTURE_PROMPT (strict JSON, no explanation, no markdown)
{
  "name": "Store Name",
  "slug": "store-name",
  "needs_auth": false,
  "colorScheme": {"primary":"#hex","secondary":"#hex","accent":"#hex","background":"#hex","surface":"#hex","text":"#hex"},
  "darkColorScheme": {"primary":"#hex","secondary":"#hex","accent":"#hex","background":"#hex","surface":"#hex","text":"#hex"},
  "typography": {"heading_font":"Inter","body_font":"Inter"},
  "data_models": [],
  "files": [
    {"path":"app/layout.tsx","type":"tsx","description":"Root layout"},
    {"path":"app/page.tsx","type":"tsx","description":"Homepage with featured products"},
    {"path":"app/globals.css","type":"css","description":"Global styles"},
    {"path":"app/products/page.tsx","type":"tsx","description":"Products listing with filters"},
    {"path":"app/products/[slug]/page.tsx","type":"tsx","description":"Product detail page"},
    {"path":"app/cart/page.tsx","type":"tsx","description":"Shopping cart page"},
    {"path":"app/checkout/page.tsx","type":"tsx","description":"Checkout form"},
    {"path":"app/checkout/success/page.tsx","type":"tsx","description":"Order confirmation page"},
    {"path":"app/api/products/route.ts","type":"ts","description":"Products API route"},
    {"path":"app/api/cart/route.ts","type":"ts","description":"Cart API route"},
    {"path":"app/api/checkout/route.ts","type":"ts","description":"Checkout API route"},
    {"path":"components/header.tsx","type":"tsx","description":"Header with cart icon and count badge"},
    {"path":"components/footer.tsx","type":"tsx","description":"Footer"},
    {"path":"components/product-card.tsx","type":"tsx","description":"Product card component"}
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
6. DO NOT include package.json, tsconfig.json, postcss.config.mjs, next.config.mjs, lib/db.ts, or lib/schema.ts — these are template-managed and injected automatically.
7. Required files: app/layout.tsx, app/page.tsx, app/globals.css
8. CRITICAL: Every component or module that is imported by any page MUST be included in the file list. If page.tsx imports "@/components/project-card", you MUST include "components/project-card.tsx" in the plan. Missing files will cause build failures.
9. Prefer inline implementations over importing from separate utility/lib files. Only create separate files for reusable components or large data modules.

### Output format: Same as NEXTJS_STRUCTURE_PROMPT (strict JSON, no explanation, no markdown)`

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
