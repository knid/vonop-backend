package constants

// ai_ecommerce_templates.go — Pre-built e-commerce React component templates
// Auto-injected for e-commerce projects into components/ecommerce/

const ECOMMERCE_TEMPLATE_PRODUCT_CARD = `"use client";

import React from "react";
import Image from "next/image";
import Link from "next/link";
import { ShoppingBag, ShoppingCart, Heart } from "lucide-react";
import { motion } from "framer-motion";
import { formatPrice, useCart, type StorefrontProduct } from "@/lib/wemob-store";

interface ProductCardProps {
  product: StorefrontProduct;
  onQuickAdd?: () => void;
}

export function ProductCard({ product, onQuickAdd }: ProductCardProps) {
  const { addItem } = useCart();
  const [isAdding, setIsAdding] = React.useState(false);
  const image = product.images?.[0];
  const hasRange = product.min_price_cents !== product.max_price_cents;
  const defaultVariant = product.variants?.[0];
  const onSale = defaultVariant?.compare_at_price > 0 && defaultVariant.compare_at_price > defaultVariant.price_in_cents;
  const outOfStock = product.variants?.length > 0 && product.variants.every((v) => !v.in_stock);

  const handleQuickAdd = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (outOfStock) return;
    setIsAdding(true);
    try {
      if (onQuickAdd) {
        onQuickAdd();
      } else if (defaultVariant) {
        await addItem(product.id, defaultVariant.id, 1);
      }
    } catch {
      // silently fail — toast handled by parent
    } finally {
      setIsAdding(false);
    }
  };

  return (
    <Link href={"/products/" + product.slug} className="group block">
      <motion.div
        whileHover={{ y: -4 }}
        transition={{ duration: 0.2 }}
        className="overflow-hidden rounded-xl border border-border bg-card"
      >
        <div className="relative aspect-square overflow-hidden bg-muted">
          {image ? (
            <Image
              src={image}
              alt={product.name}
              fill
              className="object-cover transition-transform duration-300 group-hover:scale-105"
              sizes="(max-width: 640px) 100vw, (max-width: 1024px) 50vw, 25vw"
              loading="lazy"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-muted">
              <ShoppingBag className="h-12 w-12 text-muted-foreground/40" />
            </div>
          )}
          {outOfStock && (
            <div className="absolute left-2 top-2 rounded-full bg-destructive px-2 py-0.5 text-xs font-medium text-destructive-foreground">
              Out of Stock
            </div>
          )}
          {onSale && !outOfStock && (
            <div className="absolute left-2 top-2 rounded-full bg-red-500 px-2 py-0.5 text-xs font-medium text-white">
              Sale
            </div>
          )}
          {!outOfStock && (
            <button
              onClick={handleQuickAdd}
              disabled={isAdding}
              className="absolute bottom-2 right-2 flex h-9 w-9 items-center justify-center rounded-full bg-primary text-primary-foreground opacity-0 shadow-lg transition-opacity group-hover:opacity-100 disabled:opacity-50"
              aria-label={"Add " + product.name + " to cart"}
            >
              <ShoppingCart className="h-4 w-4" />
            </button>
          )}
        </div>
        <div className="p-3">
          {product.category && (
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {product.category}
            </p>
          )}
          <h3 className="truncate text-sm font-semibold text-foreground">{product.name}</h3>
          <div className="mt-1 flex items-center gap-2">
            {onSale && defaultVariant ? (
              <>
                <span className="text-sm font-bold text-red-500">{formatPrice(defaultVariant.price_in_cents)}</span>
                <span className="text-xs text-muted-foreground line-through">{formatPrice(defaultVariant.compare_at_price)}</span>
              </>
            ) : hasRange ? (
              <span className="text-sm font-bold text-foreground">
                {formatPrice(product.min_price_cents)} – {formatPrice(product.max_price_cents)}
              </span>
            ) : (
              <span className="text-sm font-bold text-foreground">{formatPrice(product.min_price_cents)}</span>
            )}
          </div>
          {product.average_rating !== undefined && product.average_rating > 0 && (
            <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
              <span className="text-yellow-500">★</span>
              <span>{product.average_rating.toFixed(1)}</span>
              {product.review_count !== undefined && <span>({product.review_count})</span>}
            </div>
          )}
        </div>
      </motion.div>
    </Link>
  );
}
`

const ECOMMERCE_TEMPLATE_CART_DRAWER = `"use client";

import React from "react";
import Link from "next/link";
import { X, ShoppingBag } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, formatPrice } from "@/lib/wemob-store";
import { CartItemRow } from "@/components/ecommerce/cart-item-row";
import { EmptyState } from "@/components/ecommerce/empty-state";

interface CartDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CartDrawer({ isOpen, onClose }: CartDrawerProps) {
  const { cart, isLoading, itemCount } = useCart();

  // Close on escape key
  React.useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    if (isOpen) {
      document.addEventListener("keydown", handleEscape);
      document.body.style.overflow = "hidden";
    }
    return () => {
      document.removeEventListener("keydown", handleEscape);
      document.body.style.overflow = "";
    };
  }, [isOpen, onClose]);

  return (
    <AnimatePresence>
      {isOpen && (
        <>
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm"
            onClick={onClose}
          />
          <motion.div
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ type: "spring", damping: 25, stiffness: 200 }}
            className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col bg-background shadow-2xl"
          >
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <h2 className="text-lg font-semibold">Cart ({itemCount})</h2>
              <button onClick={onClose} className="rounded-full p-1 hover:bg-muted" aria-label="Close cart">
                <X className="h-5 w-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto px-4 py-4">
              {isLoading ? (
                <div className="space-y-4">
                  {[1, 2, 3].map((i) => (
                    <div key={i} className="h-20 animate-pulse rounded-lg bg-muted" />
                  ))}
                </div>
              ) : !cart || cart.items.length === 0 ? (
                <EmptyState
                  icon={<ShoppingBag className="h-12 w-12" />}
                  title="Your cart is empty"
                  description="Add some items to get started"
                />
              ) : (
                <div className="space-y-3">
                  {cart.items.map((item) => (
                    <CartItemRow key={item.id} item={item} compact />
                  ))}
                </div>
              )}
            </div>

            {cart && cart.items.length > 0 && (
              <div className="border-t border-border px-4 py-4">
                <div className="mb-3 flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Subtotal</span>
                  <span className="font-semibold">{formatPrice(cart.subtotal_cents)}</span>
                </div>
                {cart.discount_cents > 0 && (
                  <div className="mb-3 flex items-center justify-between text-sm">
                    <span className="text-green-600">Discount</span>
                    <span className="font-semibold text-green-600">-{formatPrice(cart.discount_cents)}</span>
                  </div>
                )}
                <div className="mb-4 flex items-center justify-between">
                  <span className="font-semibold">Total</span>
                  <span className="text-lg font-bold">{formatPrice(cart.total_cents)}</span>
                </div>
                <div className="flex flex-col gap-2">
                  <Link
                    href="/checkout"
                    onClick={onClose}
                    className="flex w-full items-center justify-center rounded-lg bg-primary px-4 py-3 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90"
                  >
                    Checkout
                  </Link>
                  <Link
                    href="/cart"
                    onClick={onClose}
                    className="flex w-full items-center justify-center rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
                  >
                    View Cart
                  </Link>
                </div>
              </div>
            )}
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
`

const ECOMMERCE_TEMPLATE_PRICE_DISPLAY = `"use client";

import React from "react";
import { formatPrice, type StorefrontProduct, type StorefrontVariant } from "@/lib/wemob-store";

interface PriceDisplayProps {
  product?: StorefrontProduct;
  variant?: StorefrontVariant;
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function PriceDisplay({ product, variant, size = "md", className = "" }: PriceDisplayProps) {
  const sizeClasses = {
    sm: "text-sm",
    md: "text-base",
    lg: "text-2xl",
  };

  // Single variant display
  if (variant) {
    const onSale = variant.compare_at_price > 0 && variant.compare_at_price > variant.price_in_cents;
    return (
      <div className={"flex items-center gap-2 " + className}>
        <span className={sizeClasses[size] + " font-bold " + (onSale ? "text-red-500" : "text-foreground")}>
          {formatPrice(variant.price_in_cents)}
        </span>
        {onSale && (
          <span className={"text-muted-foreground line-through " + (size === "lg" ? "text-base" : "text-xs")}>
            {formatPrice(variant.compare_at_price)}
          </span>
        )}
        {!variant.in_stock && (
          <span className="rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
            Out of Stock
          </span>
        )}
      </div>
    );
  }

  // Product-level display (range or single)
  if (product) {
    const hasRange = product.min_price_cents !== product.max_price_cents;
    const defaultVariant = product.variants?.[0];
    const onSale = defaultVariant?.compare_at_price > 0 && defaultVariant.compare_at_price > defaultVariant.price_in_cents;

    return (
      <div className={"flex items-center gap-2 " + className}>
        {onSale && defaultVariant ? (
          <>
            <span className={sizeClasses[size] + " font-bold text-red-500"}>
              {formatPrice(defaultVariant.price_in_cents)}
            </span>
            <span className={"text-muted-foreground line-through " + (size === "lg" ? "text-base" : "text-xs")}>
              {formatPrice(defaultVariant.compare_at_price)}
            </span>
          </>
        ) : hasRange ? (
          <span className={sizeClasses[size] + " font-bold text-foreground"}>
            {formatPrice(product.min_price_cents)} – {formatPrice(product.max_price_cents)}
          </span>
        ) : (
          <span className={sizeClasses[size] + " font-bold text-foreground"}>
            {formatPrice(product.min_price_cents)}
          </span>
        )}
      </div>
    );
  }

  return null;
}
`

const ECOMMERCE_TEMPLATE_QUANTITY_SELECTOR = `"use client";

import React from "react";
import { Minus, Plus } from "lucide-react";

interface QuantitySelectorProps {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  size?: "sm" | "md";
  disabled?: boolean;
}

export function QuantitySelector({
  value,
  onChange,
  min = 1,
  max = 99,
  size = "md",
  disabled = false,
}: QuantitySelectorProps) {
  const btnSize = size === "sm" ? "h-7 w-7" : "h-9 w-9";
  const textSize = size === "sm" ? "text-sm w-8" : "text-base w-10";
  const iconSize = size === "sm" ? "h-3 w-3" : "h-4 w-4";

  return (
    <div className="inline-flex items-center rounded-lg border border-border">
      <button
        onClick={() => onChange(Math.max(min, value - 1))}
        disabled={disabled || value <= min}
        className={"flex items-center justify-center rounded-l-lg transition-colors hover:bg-muted disabled:opacity-40 " + btnSize}
        aria-label="Decrease quantity"
      >
        <Minus className={iconSize} />
      </button>
      <span className={"text-center font-medium tabular-nums " + textSize}>{value}</span>
      <button
        onClick={() => onChange(Math.min(max, value + 1))}
        disabled={disabled || value >= max}
        className={"flex items-center justify-center rounded-r-lg transition-colors hover:bg-muted disabled:opacity-40 " + btnSize}
        aria-label="Increase quantity"
      >
        <Plus className={iconSize} />
      </button>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCT_GALLERY = `"use client";

import React from "react";
import Image from "next/image";
import { ShoppingBag, ChevronLeft, ChevronRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface ProductGalleryProps {
  images: string[];
  productName: string;
}

export function ProductGallery({ images, productName }: ProductGalleryProps) {
  const [selectedIndex, setSelectedIndex] = React.useState(0);

  if (!images || images.length === 0) {
    return (
      <div className="flex aspect-square w-full items-center justify-center rounded-xl bg-muted">
        <ShoppingBag className="h-16 w-16 text-muted-foreground/40" />
      </div>
    );
  }

  const handlePrev = () => setSelectedIndex((i) => (i > 0 ? i - 1 : images.length - 1));
  const handleNext = () => setSelectedIndex((i) => (i < images.length - 1 ? i + 1 : 0));

  return (
    <div className="space-y-3">
      <div className="group relative aspect-square overflow-hidden rounded-xl bg-muted">
        <AnimatePresence mode="wait">
          <motion.div
            key={selectedIndex}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="absolute inset-0"
          >
            <Image
              src={images[selectedIndex]}
              alt={productName + " - Image " + (selectedIndex + 1)}
              fill
              className="object-cover"
              sizes="(max-width: 768px) 100vw, 50vw"
              priority={selectedIndex === 0}
            />
          </motion.div>
        </AnimatePresence>
        {images.length > 1 && (
          <>
            <button
              onClick={handlePrev}
              className="absolute left-2 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-full bg-background/80 opacity-0 shadow transition-opacity group-hover:opacity-100"
              aria-label="Previous image"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <button
              onClick={handleNext}
              className="absolute right-2 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-full bg-background/80 opacity-0 shadow transition-opacity group-hover:opacity-100"
              aria-label="Next image"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          </>
        )}
      </div>
      {images.length > 1 && (
        <div className="flex gap-2 overflow-x-auto pb-1">
          {images.map((img, i) => (
            <button
              key={i}
              onClick={() => setSelectedIndex(i)}
              className={"relative h-16 w-16 flex-shrink-0 overflow-hidden rounded-lg border-2 transition-all " +
                (i === selectedIndex ? "border-primary ring-1 ring-primary" : "border-transparent opacity-60 hover:opacity-100")}
              aria-label={"View image " + (i + 1)}
            >
              <Image src={img} alt={productName + " thumbnail " + (i + 1)} fill className="object-cover" sizes="64px" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_FORM = `"use client";

import React from "react";
import { useRouter } from "next/navigation";
import { Loader2, AlertTriangle } from "lucide-react";
import { useCart, useStoreConfig, renderPaymentForm, formatPrice, type CheckoutParams, type ShippingAddress } from "@/lib/wemob-store";
import { OrderSummary } from "@/components/ecommerce/order-summary";

export function CheckoutForm() {
  const router = useRouter();
  const { cart, checkout: doCheckout, isLoading: cartLoading } = useCart();
  const { config, isLoading: configLoading } = useStoreConfig();
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [error, setError] = React.useState("");
  const [selectedProvider, setSelectedProvider] = React.useState("");
  const paymentFormRef = React.useRef<HTMLDivElement>(null);

  const [form, setForm] = React.useState({
    email: "",
    name: "",
    line1: "",
    line2: "",
    city: "",
    state: "",
    postal_code: "",
    country: "",
  });

  const providers = config?.payment_providers || [];

  React.useEffect(() => {
    if (providers.length === 1) setSelectedProvider(providers[0]);
  }, [providers.length]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setForm((prev) => ({ ...prev, [e.target.name]: e.target.value }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setIsSubmitting(true);

    const shippingAddress: ShippingAddress = {
      line1: form.line1,
      line2: form.line2 || undefined,
      city: form.city,
      state: form.state,
      postal_code: form.postal_code,
      country: form.country,
    };

    try {
      const result = await doCheckout({
        sessionId: "",
        customerEmail: form.email,
        customerName: form.name,
        shippingAddress,
        paymentProvider: selectedProvider || "cod",
      });

      // Iyzico — render the payment form
      if (result.client_secret && paymentFormRef.current) {
        renderPaymentForm(paymentFormRef.current, result.client_secret);
        return;
      }

      // COD / no payment — go to success
      if (!result.payment_url) {
        router.push("/checkout/success?order_id=" + result.order_id);
      }
      // Stripe: auto-redirects via SDK
    } catch (err) {
      setError(err instanceof Error ? err.message : "Checkout failed. Please try again.");
    } finally {
      setIsSubmitting(false);
    }
  };

  if (cartLoading || configLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!cart || cart.items.length === 0) {
    return (
      <div className="py-12 text-center">
        <p className="text-muted-foreground">Your cart is empty. Add items before checking out.</p>
      </div>
    );
  }

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_400px]">
      <form onSubmit={handleSubmit} className="space-y-6">
        <div className="rounded-xl border border-border p-6">
          <h2 className="mb-4 text-lg font-semibold">Contact Information</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="email" className="mb-1 block text-sm font-medium">Email</label>
              <input id="email" name="email" type="email" required value={form.email} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="name" className="mb-1 block text-sm font-medium">Full Name</label>
              <input id="name" name="name" type="text" required value={form.name} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-border p-6">
          <h2 className="mb-4 text-lg font-semibold">Shipping Address</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <label htmlFor="line1" className="mb-1 block text-sm font-medium">Address Line 1</label>
              <input id="line1" name="line1" type="text" required value={form.line1} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div className="sm:col-span-2">
              <label htmlFor="line2" className="mb-1 block text-sm font-medium">Address Line 2 (Optional)</label>
              <input id="line2" name="line2" type="text" value={form.line2} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div>
              <label htmlFor="city" className="mb-1 block text-sm font-medium">City</label>
              <input id="city" name="city" type="text" required value={form.city} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div>
              <label htmlFor="state" className="mb-1 block text-sm font-medium">State / Province</label>
              <input id="state" name="state" type="text" required value={form.state} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div>
              <label htmlFor="postal_code" className="mb-1 block text-sm font-medium">Postal Code</label>
              <input id="postal_code" name="postal_code" type="text" required value={form.postal_code} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div>
              <label htmlFor="country" className="mb-1 block text-sm font-medium">Country</label>
              <input id="country" name="country" type="text" required value={form.country} onChange={handleChange} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
          </div>
        </div>

        {providers.length > 0 ? (
          <div className="rounded-xl border border-border p-6">
            <h2 className="mb-4 text-lg font-semibold">Payment Method</h2>
            <div className="space-y-3">
              {providers.map((p) => (
                <label key={p} className={"flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors " + (selectedProvider === p ? "border-primary bg-primary/5" : "border-border hover:bg-muted")}>
                  <input type="radio" name="payment" value={p} checked={selectedProvider === p} onChange={() => setSelectedProvider(p)} className="h-4 w-4 accent-primary" />
                  <span className="text-sm font-medium capitalize">{p === "cod" ? "Cash on Delivery" : p}</span>
                </label>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
            <AlertTriangle className="h-5 w-5 flex-shrink-0 text-amber-600" />
            <p className="text-sm text-amber-800 dark:text-amber-200">
              Payment not yet configured — contact the store owner.
            </p>
          </div>
        )}

        {error && (
          <div className="rounded-lg bg-destructive/10 px-4 py-3 text-sm text-destructive">{error}</div>
        )}

        <button
          type="submit"
          disabled={isSubmitting || (!selectedProvider && providers.length > 0)}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-6 py-3 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
        >
          {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {isSubmitting ? "Processing..." : "Place Order" + (cart ? " · " + formatPrice(cart.total_cents) : "")}
        </button>

        <div ref={paymentFormRef} />
      </form>

      <div className="lg:sticky lg:top-4 lg:self-start">
        <OrderSummary cart={cart} />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CATEGORY_NAV = `"use client";

import React from "react";
import { motion } from "framer-motion";
import { useCategories, type StorefrontCategory } from "@/lib/wemob-store";

interface CategoryNavProps {
  selectedCategory?: string;
  onSelect: (slug: string | undefined) => void;
}

export function CategoryNav({ selectedCategory, onSelect }: CategoryNavProps) {
  const { categories, isLoading } = useCategories();

  if (isLoading) {
    return (
      <div className="flex gap-2 overflow-x-auto pb-2">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-9 w-24 animate-pulse rounded-full bg-muted" />
        ))}
      </div>
    );
  }

  if (!categories || categories.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-2">
      <button
        onClick={() => onSelect(undefined)}
        className={"relative rounded-full px-4 py-2 text-sm font-medium transition-colors " +
          (!selectedCategory ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-muted/80")}
      >
        All
      </button>
      {categories.map((cat) => (
        <button
          key={cat.id}
          onClick={() => onSelect(cat.slug)}
          className={"relative rounded-full px-4 py-2 text-sm font-medium transition-colors " +
            (selectedCategory === cat.slug ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-muted/80")}
        >
          {cat.name}
        </button>
      ))}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CART_ITEM_ROW = `"use client";

import React from "react";
import Image from "next/image";
import { Trash2, ShoppingBag } from "lucide-react";
import { useCart, formatPrice, type StorefrontCartItem } from "@/lib/wemob-store";
import { QuantitySelector } from "@/components/ecommerce/quantity-selector";

interface CartItemRowProps {
  item: StorefrontCartItem;
  compact?: boolean;
}

export function CartItemRow({ item, compact = false }: CartItemRowProps) {
  const { updateItem, removeItem } = useCart();
  const [isRemoving, setIsRemoving] = React.useState(false);

  const handleQuantityChange = async (qty: number) => {
    try {
      await updateItem(item.id, qty);
    } catch {
      // silently handle — optimistic update will rollback
    }
  };

  const handleRemove = async () => {
    setIsRemoving(true);
    try {
      await removeItem(item.id);
    } catch {
      setIsRemoving(false);
    }
  };

  return (
    <div className={"flex gap-3 rounded-lg " + (compact ? "p-2" : "border border-border p-4") + (isRemoving ? " opacity-50" : "")}>
      <div className={"relative flex-shrink-0 overflow-hidden rounded-lg bg-muted " + (compact ? "h-16 w-16" : "h-20 w-20")}>
        {item.product_image ? (
          <Image src={item.product_image} alt={item.product_name} fill className="object-cover" sizes={compact ? "64px" : "80px"} />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <ShoppingBag className="h-5 w-5 text-muted-foreground/40" />
          </div>
        )}
      </div>
      <div className="flex flex-1 flex-col justify-between">
        <div>
          <h3 className={"font-medium " + (compact ? "text-sm" : "text-base")}>{item.product_name}</h3>
          {item.variant_name && item.variant_name !== "Default" && (
            <p className="text-xs text-muted-foreground">{item.variant_name}</p>
          )}
        </div>
        <div className="flex items-center justify-between">
          <QuantitySelector value={item.quantity} onChange={handleQuantityChange} size={compact ? "sm" : "md"} />
          <div className="flex items-center gap-3">
            <span className={"font-semibold " + (compact ? "text-sm" : "text-base")}>{formatPrice(item.total_cents)}</span>
            <button onClick={handleRemove} disabled={isRemoving} className="text-muted-foreground transition-colors hover:text-destructive" aria-label={"Remove " + item.product_name}>
              <Trash2 className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_ORDER_SUMMARY = `"use client";

import React from "react";
import { formatPrice, type StorefrontCart } from "@/lib/wemob-store";

interface OrderSummaryProps {
  cart: StorefrontCart;
  className?: string;
}

export function OrderSummary({ cart, className = "" }: OrderSummaryProps) {
  return (
    <div className={"rounded-xl border border-border p-6 " + className}>
      <h3 className="mb-4 text-lg font-semibold">Order Summary</h3>
      <div className="space-y-3">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">
            Subtotal ({cart.items.reduce((sum, i) => sum + i.quantity, 0)} items)
          </span>
          <span>{formatPrice(cart.subtotal_cents)}</span>
        </div>
        {cart.discount_cents > 0 && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-green-600">
              Discount{cart.discount_code ? " (" + cart.discount_code + ")" : ""}
            </span>
            <span className="text-green-600">-{formatPrice(cart.discount_cents)}</span>
          </div>
        )}
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">Shipping</span>
          <span className="text-muted-foreground">Calculated at checkout</span>
        </div>
        <div className="border-t border-border pt-3">
          <div className="flex items-center justify-between">
            <span className="font-semibold">Total</span>
            <span className="text-xl font-bold">{formatPrice(cart.total_cents)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_EMPTY_STATE = `"use client";

import React from "react";

interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      {icon && <div className="mb-4 text-muted-foreground/40">{icon}</div>}
      <h3 className="mb-1 text-lg font-semibold text-foreground">{title}</h3>
      {description && <p className="mb-4 max-w-sm text-sm text-muted-foreground">{description}</p>}
      {action && <div>{action}</div>}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_REVIEW_LIST = `"use client";

import React from "react";
import { Star, Loader2 } from "lucide-react";
import { useReviews } from "@/lib/wemob-store";

interface ReviewListProps {
  productSlug: string;
}

function StarRating({ rating, size = "sm" }: { rating: number; size?: "sm" | "md" }) {
  const starSize = size === "sm" ? "h-3.5 w-3.5" : "h-5 w-5";
  return (
    <div className="flex gap-0.5">
      {[1, 2, 3, 4, 5].map((i) => (
        <Star
          key={i}
          className={starSize + " " + (i <= rating ? "fill-yellow-400 text-yellow-400" : "fill-muted text-muted")}
        />
      ))}
    </div>
  );
}

function StarInput({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const [hover, setHover] = React.useState(0);
  return (
    <div className="flex gap-1">
      {[1, 2, 3, 4, 5].map((i) => (
        <button
          key={i}
          type="button"
          onClick={() => onChange(i)}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(0)}
          className="p-0.5"
          aria-label={i + " star" + (i > 1 ? "s" : "")}
        >
          <Star className={"h-6 w-6 " + (i <= (hover || value) ? "fill-yellow-400 text-yellow-400" : "fill-muted text-muted")} />
        </button>
      ))}
    </div>
  );
}

export function ReviewList({ productSlug }: ReviewListProps) {
  const { reviews, averageRating, reviewCount, isLoading, submitReview } = useReviews(productSlug);
  const [showForm, setShowForm] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [submitted, setSubmitted] = React.useState(false);
  const [form, setForm] = React.useState({ name: "", email: "", rating: 5, title: "", body: "" });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.rating || !form.name || !form.email) return;
    setSubmitting(true);
    try {
      await submitReview({ customer_name: form.name, customer_email: form.email, rating: form.rating, title: form.title, body: form.body });
      setSubmitted(true);
      setShowForm(false);
      setForm({ name: "", email: "", rating: 5, title: "", body: "" });
    } catch {
      // silently handle
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-semibold">Customer Reviews</h3>
          {reviewCount > 0 && (
            <div className="mt-1 flex items-center gap-2">
              <StarRating rating={Math.round(averageRating)} size="md" />
              <span className="text-sm text-muted-foreground">
                {averageRating.toFixed(1)} out of 5 ({reviewCount} review{reviewCount !== 1 ? "s" : ""})
              </span>
            </div>
          )}
        </div>
        {!showForm && !submitted && (
          <button onClick={() => setShowForm(true)} className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90">
            Write a Review
          </button>
        )}
      </div>

      {submitted && (
        <div className="rounded-lg bg-green-50 px-4 py-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-200">
          Thank you for your review! It has been submitted successfully.
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} className="space-y-4 rounded-xl border border-border p-4">
          <div>
            <label className="mb-1 block text-sm font-medium">Rating</label>
            <StarInput value={form.rating} onChange={(v) => setForm((f) => ({ ...f, rating: v }))} />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="review-name" className="mb-1 block text-sm font-medium">Name</label>
              <input id="review-name" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div>
              <label htmlFor="review-email" className="mb-1 block text-sm font-medium">Email</label>
              <input id="review-email" type="email" required value={form.email} onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
          </div>
          <div>
            <label htmlFor="review-title" className="mb-1 block text-sm font-medium">Title (optional)</label>
            <input id="review-title" value={form.title} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
          </div>
          <div>
            <label htmlFor="review-body" className="mb-1 block text-sm font-medium">Review</label>
            <textarea id="review-body" rows={3} value={form.body} onChange={(e) => setForm((f) => ({ ...f, body: e.target.value }))} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
          </div>
          <div className="flex gap-2">
            <button type="submit" disabled={submitting} className="flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50">
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              Submit Review
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="rounded-lg border border-border px-4 py-2 text-sm font-medium hover:bg-muted">
              Cancel
            </button>
          </div>
        </form>
      )}

      {isLoading ? (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-lg bg-muted" />
          ))}
        </div>
      ) : reviews.length === 0 && !submitted ? (
        <p className="py-6 text-center text-sm text-muted-foreground">No reviews yet. Be the first to review this product!</p>
      ) : (
        <div className="space-y-4">
          {reviews.map((review) => (
            <div key={review.id} className="rounded-lg border border-border p-4">
              <div className="mb-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <StarRating rating={review.rating} />
                  {review.is_verified && (
                    <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-800 dark:bg-green-900 dark:text-green-200">Verified</span>
                  )}
                </div>
                <span className="text-xs text-muted-foreground">
                  {new Date(review.created_at).toLocaleDateString()}
                </span>
              </div>
              {review.title && <h4 className="mb-1 font-medium">{review.title}</h4>}
              {review.body && <p className="text-sm text-muted-foreground">{review.body}</p>}
              <p className="mt-2 text-xs text-muted-foreground">by {review.customer_name}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_WISHLIST_BUTTON = `"use client";

import React from "react";
import { Heart } from "lucide-react";
import { useWishlist } from "@/lib/wemob-store";

interface WishlistButtonProps {
  productId: string;
  size?: "sm" | "md";
  className?: string;
}

export function WishlistButton({ productId, size = "md", className = "" }: WishlistButtonProps) {
  const { isInWishlist, toggleWishlist } = useWishlist();
  const [isToggling, setIsToggling] = React.useState(false);
  const active = isInWishlist(productId);

  const handleClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (isToggling) return;
    setIsToggling(true);
    try {
      await toggleWishlist(productId);
    } catch {
      // silently handle
    } finally {
      setIsToggling(false);
    }
  };

  const btnSize = size === "sm" ? "h-8 w-8" : "h-10 w-10";
  const iconSize = size === "sm" ? "h-4 w-4" : "h-5 w-5";

  return (
    <button
      onClick={handleClick}
      disabled={isToggling}
      className={"flex items-center justify-center rounded-full border transition-all " +
        (active ? "border-red-200 bg-red-50 text-red-500 dark:border-red-800 dark:bg-red-950" : "border-border bg-background text-muted-foreground hover:text-red-500") +
        " " + btnSize + " " + className}
      aria-label={active ? "Remove from wishlist" : "Add to wishlist"}
    >
      <Heart className={iconSize + (active ? " fill-current" : "")} />
    </button>
  );
}
`
