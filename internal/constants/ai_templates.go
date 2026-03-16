package constants

// ai_templates.go contains template file contents and path arrays for generated
// Next.js projects. These are injected as-is into generated projects.

// Template file paths that are managed by hardcoded templates, not LLM-generated.
var NEXTJS_TEMPLATE_FILE_PATHS = []string{
	"package.json",
	"tsconfig.json",
	"postcss.config.mjs",
	"next.config.mjs",
	"app/globals.css",
	"app/not-found.tsx",
	"app/error.tsx",
	".gitignore",
}

// NEXTJS_DB_TEMPLATE_FILE_PATHS are template-managed files injected only after successful Neon provisioning.
var NEXTJS_DB_TEMPLATE_FILE_PATHS = []string{
	"lib/db.ts",
	"lib/schema.ts",
}

// NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS are template-managed files injected only for e-commerce projects.
var NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS = []string{
	"lib/wemob-store.ts",
	"components/ecommerce/product-card.tsx",
	"components/ecommerce/cart-drawer.tsx",
	"components/ecommerce/price-display.tsx",
	"components/ecommerce/quantity-selector.tsx",
	"components/ecommerce/product-gallery.tsx",
	"components/ecommerce/checkout-form.tsx",
	"components/ecommerce/category-nav.tsx",
	"components/ecommerce/cart-item-row.tsx",
	"components/ecommerce/order-summary.tsx",
	"components/ecommerce/empty-state.tsx",
	"components/ecommerce/review-list.tsx",
	"components/ecommerce/wishlist-button.tsx",
	"components/ecommerce/site-header.tsx",
	"components/ecommerce/site-footer.tsx",
	"components/ecommerce/providers.tsx",
	"components/ecommerce/newsletter-section.tsx",
	"app/cart/page.tsx",
	"app/checkout/page.tsx",
	"app/checkout/success/page.tsx",
	"app/checkout/cancel/page.tsx",
	"app/products/page.tsx",
	"app/products/[slug]/page.tsx",
	"app/categories/[slug]/page.tsx",
	"app/policies/shipping/page.tsx",
	"app/policies/returns/page.tsx",
	"app/policies/privacy/page.tsx",
	"app/policies/terms/page.tsx",
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
  devIndicators: false,
  images: {
    unoptimized: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
  eslint: {
    ignoreDuringBuilds: true,
  },
};

export default nextConfig;
`

const NEXTJS_TEMPLATE_GITIGNORE = `# dependencies
node_modules/
.pnp
.pnp.js

# next.js
.next/
out/

# production
build/

# misc
.DS_Store
*.pem

# debug
npm-debug.log*
yarn-debug.log*
yarn-error.log*

# env files
.env
.env.local
.env.development.local
.env.test.local
.env.production.local

# vercel
.vercel

# typescript
*.tsbuildinfo
next-env.d.ts
`

// NEXTJS_TEMPLATE_NOT_FOUND is the custom 404 page. It shows the missing path and a
// "Create with AI" button that posts a message to the parent Wemob studio to trigger
// page creation via chat.
const NEXTJS_TEMPLATE_NOT_FOUND = `"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"

export default function NotFound() {
  const pathname = usePathname()
  const [isEmbedded, setIsEmbedded] = useState(false)

  useEffect(() => {
    setIsEmbedded(window.self !== window.top)
  }, [])

  const handleCreateWithAI = () => {
    window.parent.postMessage(
      { type: "wemob-create-page", path: pathname },
      "*"
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <div className="text-center space-y-6 px-4">
        <div className="space-y-2">
          <h1 className="text-7xl font-bold text-foreground/10">404</h1>
          <p className="text-lg text-muted-foreground">This page could not be found.</p>
          {pathname && (
            <p className="text-sm text-muted-foreground/60 font-mono">{pathname}</p>
          )}
        </div>
        <div className="flex flex-col items-center gap-3">
          {isEmbedded && (
            <button
              onClick={handleCreateWithAI}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 transition-colors"
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M8 2L9.5 6.5L14 8L9.5 9.5L8 14L6.5 9.5L2 8L6.5 6.5L8 2Z" fill="currentColor" />
              </svg>
              Create with AI
            </button>
          )}
          <Link
            href="/"
            className="text-sm text-muted-foreground hover:text-foreground transition-colors underline underline-offset-4"
          >
            Back to home
          </Link>
        </div>
      </div>
    </div>
  )
}
`

// NEXTJS_TEMPLATE_ERROR is the error boundary page. It catches runtime errors and
// shows a user-friendly error screen with a retry button.
const NEXTJS_TEMPLATE_ERROR = `"use client"

import { useEffect } from "react"

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <div className="text-center space-y-6 px-4">
        <div className="space-y-2">
          <h1 className="text-7xl font-bold text-foreground/10">Error</h1>
          <p className="text-lg text-muted-foreground">Something went wrong.</p>
        </div>
        <button
          onClick={() => reset()}
          className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 transition-colors"
        >
          Try again
        </button>
      </div>
    </div>
  )
}
`

// NEXTJS_TEMPLATE_DB_TS is the database connection file using Neon serverless + Drizzle ORM.
// Uses DATABASE_URL env var only — no hardcoded connection strings.
const NEXTJS_TEMPLATE_DB_TS = `import { neon } from '@neondatabase/serverless';
import { drizzle } from 'drizzle-orm/neon-http';
import * as schema from './schema';

if (!process.env.DATABASE_URL) {
  throw new Error('DATABASE_URL environment variable is not set');
}

const sql = neon(process.env.DATABASE_URL);
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
  quantity: number
): Promise<StorefrontCart> {
  return apiFetch<StorefrontCart>("/cart/" + sessionId + "/items", {
    method: "POST",
    body: JSON.stringify({ product_id: productId, quantity }),
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
  addItem: (productId: string, quantity?: number) => Promise<void>;
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

/** Fetch reviews for a product by slug. */
export async function getProductReviews(slug: string): Promise<ReviewsResult> {
  return apiFetch<ReviewsResult>("/products/" + slug + "/reviews");
}

/** Submit a review for a product. */
export async function submitReview(
  slug: string,
  data: { customer_name: string; customer_email: string; rating: number; title?: string; body?: string }
): Promise<{ id: string; status: string; message: string }> {
  return apiFetch("/products/" + slug + "/reviews", {
    method: "POST",
    body: JSON.stringify(data),
  });
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

export interface WishlistItem {
  id: string;
  product_id: string;
  created_at: string;
}

/** Fetch wishlist items for a session. */
export async function getWishlist(sessionId: string): Promise<WishlistItem[]> {
  return apiFetch<WishlistItem[]>("/wishlist/" + sessionId);
}

/** Add a product to the wishlist. */
export async function addToWishlist(sessionId: string, productId: string): Promise<void> {
  await apiFetch("/wishlist/" + sessionId, {
    method: "POST",
    body: JSON.stringify({ product_id: productId }),
  });
}

/** Remove a product from the wishlist. */
export async function removeFromWishlist(sessionId: string, productId: string): Promise<void> {
  await apiFetch("/wishlist/" + sessionId + "/" + productId, {
    method: "DELETE",
  });
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

// ============================================================================
// Auth Templates
// ============================================================================

// NEXTJS_AUTH_TEMPLATE_FILE_PATHS are template-managed files injected only for projects that need auth.
var NEXTJS_AUTH_TEMPLATE_FILE_PATHS = []string{
	"lib/wemob-auth.ts",
	"lib/wemob-auth-client.tsx",
	"app/api/auth/[...action]/route.ts",
}

// AUTH_DEFAULT_DATA_MODELS defines the standard users table for auth-enabled projects.
const AUTH_DEFAULT_DATA_MODELS = `[
  {
    "name": "users",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "email", "type": "text", "nullable": false},
      {"name": "password_hash", "type": "text", "nullable": false},
      {"name": "name", "type": "text", "nullable": true},
      {"name": "avatar_url", "type": "text", "nullable": true},
      {"name": "role", "type": "text", "nullable": false, "default": "'user'"},
      {"name": "email_verified", "type": "boolean", "nullable": false, "default": "false"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  }
]`

// NEXTJS_TEMPLATE_WEMOB_AUTH is the server-side auth library template.
// Uses jose for JWT (edge-compatible), bcryptjs for passwords, next/headers cookies.
const NEXTJS_TEMPLATE_WEMOB_AUTH = `import { SignJWT, jwtVerify } from "jose";
import bcrypt from "bcryptjs";
import { cookies } from "next/headers";
import { db } from "./db";
import { users } from "./schema";
import { eq } from "drizzle-orm";

// --- Types ---

export interface AuthUser {
  id: string;
  email: string;
  name: string | null;
  avatarUrl: string | null;
  role: string;
  emailVerified: boolean;
}

export interface Session {
  user: AuthUser;
  expires: Date;
}

export interface AuthResult {
  success: boolean;
  error?: string;
  user?: AuthUser;
}

// --- Config ---

const SESSION_COOKIE = "wemob_auth_session";
const SESSION_DURATION = 7 * 24 * 60 * 60; // 7 days in seconds

function getSecretKey() {
  const secret = process.env.AUTH_SECRET;
  if (!secret) throw new Error("AUTH_SECRET environment variable is not set");
  return new TextEncoder().encode(secret);
}

// --- Helpers ---

function sanitizeUser(row: typeof users.$inferSelect): AuthUser {
  return {
    id: row.id,
    email: row.email,
    name: row.name,
    avatarUrl: row.avatarUrl,
    role: row.role,
    emailVerified: row.emailVerified,
  };
}

async function createSessionToken(user: AuthUser): Promise<string> {
  return new SignJWT({ sub: user.id, email: user.email, role: user.role })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(` + "`" + `${SESSION_DURATION}s` + "`" + `)
    .sign(getSecretKey());
}

async function verifySessionToken(token: string): Promise<{ sub: string } | null> {
  try {
    const { payload } = await jwtVerify(token, getSecretKey());
    return payload as { sub: string };
  } catch {
    return null;
  }
}

// --- Auth Functions ---

/** Register a new user with email and password */
export async function register(
  email: string,
  password: string,
  name?: string
): Promise<AuthResult> {
  try {
    const existing = await db
      .select()
      .from(users)
      .where(eq(users.email, email.toLowerCase().trim()))
      .limit(1);

    if (existing.length > 0) {
      return { success: false, error: "An account with this email already exists" };
    }

    const passwordHash = await bcrypt.hash(password, 12);

    const [newUser] = await db
      .insert(users)
      .values({
        email: email.toLowerCase().trim(),
        passwordHash,
        name: name || null,
        role: "user",
        emailVerified: false,
      })
      .returning();

    const user = sanitizeUser(newUser);
    const token = await createSessionToken(user);

    const cookieStore = await cookies();
    cookieStore.set(SESSION_COOKIE, token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      maxAge: SESSION_DURATION,
      path: "/",
    });

    return { success: true, user };
  } catch (error) {
    console.error("Registration error:", error);
    return { success: false, error: "Registration failed. Please try again." };
  }
}

/** Log in with email and password */
export async function login(
  email: string,
  password: string
): Promise<AuthResult> {
  try {
    const [row] = await db
      .select()
      .from(users)
      .where(eq(users.email, email.toLowerCase().trim()))
      .limit(1);

    if (!row) {
      return { success: false, error: "Invalid email or password" };
    }

    const valid = await bcrypt.compare(password, row.passwordHash);
    if (!valid) {
      return { success: false, error: "Invalid email or password" };
    }

    const user = sanitizeUser(row);
    const token = await createSessionToken(user);

    const cookieStore = await cookies();
    cookieStore.set(SESSION_COOKIE, token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      maxAge: SESSION_DURATION,
      path: "/",
    });

    return { success: true, user };
  } catch (error) {
    console.error("Login error:", error);
    return { success: false, error: "Login failed. Please try again." };
  }
}

/** Log out the current user */
export async function logout(): Promise<void> {
  const cookieStore = await cookies();
  cookieStore.delete(SESSION_COOKIE);
}

/** Get the current session (returns null if not authenticated) */
export async function getSession(): Promise<Session | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE)?.value;
  if (!token) return null;

  const payload = await verifySessionToken(token);
  if (!payload?.sub) return null;

  const [row] = await db
    .select()
    .from(users)
    .where(eq(users.id, payload.sub))
    .limit(1);

  if (!row) return null;

  return {
    user: sanitizeUser(row),
    expires: new Date(Date.now() + SESSION_DURATION * 1000),
  };
}

/** Get the current user or null */
export async function getCurrentUser(): Promise<AuthUser | null> {
  const session = await getSession();
  return session?.user || null;
}

/** Require authentication — throws redirect if not logged in. Use in server components. */
export async function requireAuth(redirectTo = "/login"): Promise<AuthUser> {
  const session = await getSession();
  if (!session) {
    const { redirect } = await import("next/navigation");
    redirect(redirectTo);
  }
  return session.user;
}

/** Get session from API route request (reads cookie from headers) */
export async function getApiSession(): Promise<Session | null> {
  return getSession();
}
`

// NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT is the client-side auth library template.
const NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT = `"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from "react";

// --- Types ---

interface AuthUser {
  id: string;
  email: string;
  name: string | null;
  avatarUrl: string | null;
  role: string;
  emailVerified: boolean;
}

interface AuthContextType {
  user: AuthUser | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<{ success: boolean; error?: string }>;
  register: (email: string, password: string, name?: string) => Promise<{ success: boolean; error?: string }>;
  logout: () => Promise<void>;
  refreshSession: () => Promise<void>;
}

// --- Context ---

const AuthContext = createContext<AuthContextType | null>(null);

// --- Provider ---

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const refreshSession = useCallback(async () => {
    try {
      const res = await fetch("/api/auth/session");
      if (res.ok) {
        const data = await res.json();
        setUser(data.user || null);
      } else {
        setUser(null);
      }
    } catch {
      setUser(null);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshSession();
  }, [refreshSession]);

  const login = useCallback(
    async (email: string, password: string) => {
      try {
        const res = await fetch("/api/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email, password }),
        });
        const data = await res.json();
        if (data.success && data.user) {
          setUser(data.user);
        }
        return { success: data.success, error: data.error };
      } catch {
        return { success: false, error: "Network error. Please try again." };
      }
    },
    []
  );

  const register = useCallback(
    async (email: string, password: string, name?: string) => {
      try {
        const res = await fetch("/api/auth/register", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email, password, name }),
        });
        const data = await res.json();
        if (data.success && data.user) {
          setUser(data.user);
        }
        return { success: data.success, error: data.error };
      } catch {
        return { success: false, error: "Network error. Please try again." };
      }
    },
    []
  );

  const logout = useCallback(async () => {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      setUser(null);
    }
  }, []);

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        login,
        register,
        logout,
        refreshSession,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

// --- Hook ---

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
`

// NEXTJS_TEMPLATE_AUTH_ROUTE is the catch-all API route for auth actions.
// Template-managed so the LLM doesn't generate a broken version.
const NEXTJS_TEMPLATE_AUTH_ROUTE = `import { NextRequest, NextResponse } from "next/server";
import { register, login, logout, getSession } from "@/lib/wemob-auth";

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ action: string[] }> }
) {
  const { action } = await params;
  const actionName = action[0];

  try {
    switch (actionName) {
      case "register": {
        const { email, password, name } = await req.json();
        if (!email || !password) {
          return NextResponse.json(
            { success: false, error: "Email and password are required" },
            { status: 400 }
          );
        }
        const result = await register(email, password, name);
        return NextResponse.json(result, {
          status: result.success ? 200 : 400,
        });
      }
      case "login": {
        const { email, password } = await req.json();
        if (!email || !password) {
          return NextResponse.json(
            { success: false, error: "Email and password are required" },
            { status: 400 }
          );
        }
        const result = await login(email, password);
        return NextResponse.json(result, {
          status: result.success ? 200 : 401,
        });
      }
      case "logout": {
        await logout();
        return NextResponse.json({ success: true });
      }
      default:
        return NextResponse.json(
          { success: false, error: "Unknown action" },
          { status: 404 }
        );
    }
  } catch (error) {
    console.error("Auth API error:", error);
    return NextResponse.json(
      { success: false, error: "Internal server error" },
      { status: 500 }
    );
  }
}

export async function GET(
  _req: NextRequest,
  { params }: { params: Promise<{ action: string[] }> }
) {
  const { action } = await params;
  const actionName = action[0];

  if (actionName === "session") {
    const session = await getSession();
    if (!session) {
      return NextResponse.json({ user: null }, { status: 200 });
    }
    return NextResponse.json({ user: session.user }, { status: 200 });
  }

  return NextResponse.json(
    { success: false, error: "Unknown action" },
    { status: 404 }
  );
}
`
