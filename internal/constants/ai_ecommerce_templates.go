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
  const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;
  const outOfStock = product.in_stock === false;

  const handleQuickAdd = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (outOfStock) return;
    setIsAdding(true);
    try {
      if (onQuickAdd) {
        onQuickAdd();
      } else {
        await addItem(product.id, 1);
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
            {onSale ? (
              <>
                <span className="text-sm font-bold text-red-500">{formatPrice(product.price_in_cents)}</span>
                <span className="text-xs text-muted-foreground line-through">{formatPrice(product.compare_at_price)}</span>
              </>
            ) : (
              <span className="text-sm font-bold text-foreground">{formatPrice(product.price_in_cents)}</span>
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
import { formatPrice, type StorefrontProduct } from "@/lib/wemob-store";

interface PriceDisplayProps {
  product?: StorefrontProduct;
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function PriceDisplay({ product, size = "md", className = "" }: PriceDisplayProps) {
  const sizeClasses = {
    sm: "text-sm",
    md: "text-base",
    lg: "text-2xl",
  };

  if (!product) return null;

  const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;

  return (
    <div className={"flex items-center gap-2 " + className}>
      <span className={sizeClasses[size] + " font-bold " + (onSale ? "text-red-500" : "text-foreground")}>
        {formatPrice(product.price_in_cents)}
      </span>
      {onSale && (
        <span className={"text-muted-foreground line-through " + (size === "lg" ? "text-base" : "text-xs")}>
          {formatPrice(product.compare_at_price)}
        </span>
      )}
      {product.in_stock === false && (
        <span className="rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
          Out of Stock
        </span>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_QUANTITY_SELECTOR = `"use client";

import React, { useState } from "react";
import { Minus, Plus } from "lucide-react";

interface QuantitySelectorProps {
  value?: number;
  onChange?: (value: number) => void;
  min?: number;
  max?: number;
  size?: "sm" | "md";
  disabled?: boolean;
}

export function QuantitySelector({
  value: controlledValue,
  onChange,
  min = 1,
  max = 99,
  size = "md",
  disabled = false,
}: QuantitySelectorProps) {
  const [internalValue, setInternalValue] = useState(controlledValue ?? min);
  const value = controlledValue ?? internalValue;

  const handleChange = (next: number) => {
    setInternalValue(next);
    onChange?.(next);
  };

  const btnSize = size === "sm" ? "h-7 w-7" : "h-9 w-9";
  const textSize = size === "sm" ? "text-sm w-8" : "text-base w-10";
  const iconSize = size === "sm" ? "h-3 w-3" : "h-4 w-4";

  return (
    <div className="inline-flex items-center rounded-lg border border-border">
      <button
        onClick={() => handleChange(Math.max(min, value - 1))}
        disabled={disabled || value <= min}
        className={"flex items-center justify-center rounded-l-lg transition-colors hover:bg-muted disabled:opacity-40 " + btnSize}
        aria-label="Decrease quantity"
      >
        <Minus className={iconSize} />
      </button>
      <span className={"text-center font-medium tabular-nums " + textSize}>{value}</span>
      <button
        onClick={() => handleChange(Math.min(max, value + 1))}
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

const ECOMMERCE_TEMPLATE_SITE_HEADER = `"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ShoppingBag, Search, Menu, X } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useCart, useCategories, useStoreConfig } from "@/lib/wemob-store";
import { CartDrawer } from "@/components/ecommerce/cart-drawer";

export function SiteHeader() {
  const pathname = usePathname();
  const { itemCount } = useCart();
  const { categories } = useCategories();
  const storeConfig = useStoreConfig();

  const [cartOpen, setCartOpen] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const handleScroll = () => setScrolled(window.scrollY > 10);
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    setMobileMenuOpen(false);
  }, [pathname]);

  const navLinks = [
    { label: "Home", href: "/" },
    { label: "Products", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
    { label: "About", href: "/about" },
    { label: "Contact", href: "/contact" },
  ];

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname.startsWith(href);

  return (
    <>
      <header
        className={
          "sticky top-0 z-50 w-full transition-all duration-300 " +
          (scrolled
            ? "bg-background/80 backdrop-blur-md border-b border-border shadow-sm"
            : "bg-background")
        }
      >
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link href="/" className="text-xl font-bold tracking-tight text-foreground">
            {storeConfig.name}
          </Link>

          <nav className="hidden items-center gap-1 md:flex">
            {navLinks.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className={
                  "rounded-md px-3 py-2 text-sm font-medium transition-colors " +
                  (isActive(link.href)
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground")
                }
              >
                {link.label}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-2">
            <Link
              href="/products?q="
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label="Search products"
            >
              <Search className="h-5 w-5" />
            </Link>

            <button
              onClick={() => setCartOpen(true)}
              className="relative flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label="Open cart"
            >
              <ShoppingBag className="h-5 w-5" />
              {itemCount > 0 && (
                <span className="absolute -right-0.5 -top-0.5 flex h-5 w-5 items-center justify-center rounded-full bg-primary text-[11px] font-semibold text-primary-foreground">
                  {itemCount > 99 ? "99+" : itemCount}
                </span>
              )}
            </button>

            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              className="flex h-10 w-10 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground md:hidden"
              aria-label={mobileMenuOpen ? "Close menu" : "Open menu"}
            >
              {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </button>
          </div>
        </div>

        <AnimatePresence>
          {mobileMenuOpen && (
            <motion.nav
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: "auto", opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{ duration: 0.2 }}
              className="overflow-hidden border-t border-border bg-background md:hidden"
            >
              <div className="flex flex-col gap-1 px-4 py-3">
                {navLinks.map((link) => (
                  <Link
                    key={link.href}
                    href={link.href}
                    className={
                      "rounded-md px-3 py-2.5 text-sm font-medium transition-colors " +
                      (isActive(link.href)
                        ? "bg-primary/10 text-primary"
                        : "text-muted-foreground hover:bg-accent hover:text-foreground")
                    }
                  >
                    {link.label}
                  </Link>
                ))}
              </div>
            </motion.nav>
          )}
        </AnimatePresence>
      </header>

      <CartDrawer isOpen={cartOpen} onClose={() => setCartOpen(false)} />
    </>
  );
}
`

const ECOMMERCE_TEMPLATE_SITE_FOOTER = `"use client";

import Link from "next/link";
import { useCategories, useStoreConfig } from "@/lib/wemob-store";

export function SiteFooter() {
  const { categories } = useCategories();
  const storeConfig = useStoreConfig();

  const shopLinks = [
    { label: "Products", href: "/products" },
    ...(categories ?? []).map((cat) => ({
      label: cat.name,
      href: "/categories/" + cat.slug,
    })),
  ];

  const companyLinks = [
    { label: "About", href: "/about" },
    { label: "Contact", href: "/contact" },
  ];

  const supportLinks = [
    { label: "FAQ", href: "/faq" },
    { label: "Shipping Policy", href: "/policies/shipping" },
    { label: "Returns", href: "/policies/returns" },
  ];

  const legalLinks = [
    { label: "Privacy Policy", href: "/policies/privacy" },
    { label: "Terms of Service", href: "/policies/terms" },
  ];

  return (
    <footer className="border-t border-border bg-muted">
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-2 lg:grid-cols-4">
          <div className="sm:col-span-2 lg:col-span-1">
            <Link href="/" className="text-lg font-bold tracking-tight text-foreground">
              {storeConfig.name}
            </Link>
            <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
              Quality products curated for you. Shop with confidence and enjoy a
              seamless shopping experience.
            </p>
          </div>

          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wider text-foreground">
              Shop
            </h3>
            <ul className="mt-4 space-y-2.5">
              {shopLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wider text-foreground">
              Company
            </h3>
            <ul className="mt-4 space-y-2.5">
              {companyLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>

            <h3 className="mt-8 text-sm font-semibold uppercase tracking-wider text-foreground">
              Support
            </h3>
            <ul className="mt-4 space-y-2.5">
              {supportLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wider text-foreground">
              Legal
            </h3>
            <ul className="mt-4 space-y-2.5">
              {legalLinks.map((link) => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <div className="mt-12 border-t border-border pt-8">
          <p className="text-center text-sm text-muted-foreground">
            &copy; {new Date().getFullYear()} {storeConfig.name}. All rights reserved.
          </p>
        </div>
      </div>
    </footer>
  );
}
`

const ECOMMERCE_TEMPLATE_PROVIDERS = `"use client";

import React from "react";
import { CartProvider } from "@/lib/wemob-store";

export function Providers({ children }: { children: React.ReactNode }) {
  return <CartProvider>{children}</CartProvider>;
}
`

const ECOMMERCE_TEMPLATE_NEWSLETTER = `"use client";

import React, { useState } from "react";
import { Mail, CheckCircle, ArrowRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

export function NewsletterSection() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setStatus("error");
      setMessage("Please enter a valid email address.");
      return;
    }

    setStatus("loading");
    setMessage("");

    setTimeout(() => {
      setStatus("success");
      setMessage("Thank you for subscribing! Check your inbox for a confirmation.");
      setEmail("");
    }, 1000);
  };

  return (
    <section className="py-16 sm:py-20">
      <div className="mx-auto max-w-2xl px-4 sm:px-6 lg:px-8">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.5 }}
          className="rounded-2xl border border-border bg-gradient-to-br from-primary/5 to-accent/5 p-8 text-center sm:p-12"
        >
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-primary/10">
            <Mail className="h-6 w-6 text-primary" />
          </div>

          <h2 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
            Stay in the Loop
          </h2>
          <p className="mt-2 text-muted-foreground">
            Subscribe for exclusive deals, new arrivals, and style inspiration.
          </p>

          <form onSubmit={handleSubmit} className="mt-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-center">
              <input
                type="email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  if (status === "error") setStatus("idle");
                }}
                placeholder="Enter your email"
                className="h-11 w-full rounded-lg border border-border bg-background px-4 text-sm text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 sm:max-w-xs"
                disabled={status === "loading"}
              />
              <button
                type="submit"
                disabled={status === "loading"}
                className="inline-flex h-11 items-center justify-center gap-2 rounded-lg bg-primary px-6 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
              >
                {status === "loading" ? "Subscribing..." : "Subscribe"}
                {status !== "loading" && <ArrowRight className="h-4 w-4" />}
              </button>
            </div>
          </form>

          <AnimatePresence mode="wait">
            {status === "success" && (
              <motion.div
                initial={{ opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0 }}
                className="mt-4 flex items-center justify-center gap-2 text-sm text-green-600 dark:text-green-400"
              >
                <CheckCircle className="h-4 w-4" />
                {message}
              </motion.div>
            )}
            {status === "error" && (
              <motion.p
                initial={{ opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0 }}
                className="mt-4 text-sm text-red-600 dark:text-red-400"
              >
                {message}
              </motion.p>
            )}
          </AnimatePresence>

          <p className="mt-4 text-xs text-muted-foreground">
            No spam, ever. Unsubscribe at any time.
          </p>
        </motion.div>
      </div>
    </section>
  );
}
`

const ECOMMERCE_TEMPLATE_CART_PAGE = `"use client";

import React, { useState } from "react";
import Link from "next/link";
import { ShoppingBag, ArrowLeft } from "lucide-react";
import { useCart, formatPrice } from "@/lib/wemob-store";
import { CartItemRow } from "@/components/ecommerce/cart-item-row";
import { QuantitySelector } from "@/components/ecommerce/quantity-selector";
import { OrderSummary } from "@/components/ecommerce/order-summary";
import { EmptyState } from "@/components/ecommerce/empty-state";

export default function CartPage() {
  const { cart, isLoading, itemCount, applyDiscount, removeDiscountCode } = useCart();

  const [discountCode, setDiscountCode] = useState("");
  const [discountLoading, setDiscountLoading] = useState(false);
  const [discountError, setDiscountError] = useState("");

  const handleApplyDiscount = async () => {
    if (!discountCode.trim()) return;
    setDiscountLoading(true);
    setDiscountError("");
    try {
      await applyDiscount(discountCode.trim());
    } catch {
      setDiscountError("Invalid or expired discount code.");
    } finally {
      setDiscountLoading(false);
    }
  };

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground">Shopping Cart</h1>
        <div className="mt-8 space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-lg bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (!cart || itemCount === 0) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground">Shopping Cart</h1>
        <div className="mt-12">
          <EmptyState
            icon={ShoppingBag}
            title="Your cart is empty"
            description="Looks like you haven't added anything to your cart yet."
            action={{ label: "Continue Shopping", href: "/products" }}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold tracking-tight text-foreground">
          Shopping Cart ({itemCount})
        </h1>
        <Link
          href="/products"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Continue Shopping
        </Link>
      </div>

      <div className="mt-8 gap-8 lg:grid lg:grid-cols-[1fr_400px]">
        <div>
          <div className="divide-y divide-border rounded-lg border border-border">
            {cart.items.map((item) => (
              <CartItemRow key={item.id} item={item} />
            ))}
          </div>

          <div className="mt-6 flex items-end gap-3">
            <div className="flex-1">
              <label htmlFor="discount" className="mb-1.5 block text-sm font-medium text-foreground">
                Discount Code
              </label>
              <input
                id="discount"
                type="text"
                value={discountCode}
                onChange={(e) => {
                  setDiscountCode(e.target.value);
                  setDiscountError("");
                }}
                placeholder="Enter code"
                className="h-10 w-full rounded-lg border border-border bg-background px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
              />
              {discountError && (
                <p className="mt-1 text-sm text-red-600 dark:text-red-400">{discountError}</p>
              )}
            </div>
            <button
              onClick={handleApplyDiscount}
              disabled={discountLoading || !discountCode.trim()}
              className="h-10 rounded-lg bg-secondary px-4 text-sm font-medium text-secondary-foreground transition-colors hover:bg-secondary/80 disabled:opacity-50"
            >
              {discountLoading ? "Applying..." : "Apply"}
            </button>
          </div>

          {cart.discountCode && (
            <div className="mt-3 flex items-center gap-2 text-sm">
              <span className="rounded-md bg-green-100 px-2 py-0.5 font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
                {cart.discountCode}
              </span>
              <button
                onClick={() => removeDiscountCode()}
                className="text-muted-foreground transition-colors hover:text-foreground"
              >
                Remove
              </button>
            </div>
          )}
        </div>

        <div className="mt-8 lg:mt-0">
          <OrderSummary cart={cart} />
          <Link
            href="/checkout"
            className="mt-4 flex h-12 w-full items-center justify-center rounded-lg bg-primary text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Proceed to Checkout
          </Link>
        </div>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_PAGE = `"use client";

import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { CheckoutForm } from "@/components/ecommerce/checkout-form";

export default function CheckoutPage() {
  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 lg:px-8">
      <nav className="mb-6 flex items-center gap-2 text-sm text-muted-foreground">
        <Link
          href="/cart"
          className="inline-flex items-center gap-1 transition-colors hover:text-foreground"
        >
          <ChevronLeft className="h-4 w-4" />
          Back to Cart
        </Link>
      </nav>

      <h1 className="text-3xl font-bold tracking-tight text-foreground">Checkout</h1>
      <p className="mt-1 text-muted-foreground">
        Complete your order by filling in the details below.
      </p>

      <div className="mt-8">
        <CheckoutForm />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_SUCCESS_PAGE = `"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { CheckCircle, Package, Loader2 } from "lucide-react";
import { getOrder, formatPrice } from "@/lib/wemob-store";

export default function CheckoutSuccessPage() {
  const searchParams = useSearchParams();
  const orderId = searchParams.get("order_id");

  const [order, setOrder] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!orderId) {
      setLoading(false);
      setError("No order ID provided.");
      return;
    }
    getOrder(orderId)
      .then((data) => {
        setOrder(data);
        setLoading(false);
      })
      .catch(() => {
        setError("Order not found.");
        setLoading(false);
      });
  }, [orderId]);

  if (loading) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center px-4">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        <p className="mt-4 text-muted-foreground">Loading order details...</p>
      </div>
    );
  }

  if (error || !order) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center px-4 text-center">
        <Package className="h-12 w-12 text-muted-foreground" />
        <h1 className="mt-4 text-xl font-semibold text-foreground">
          {error || "Order not found"}
        </h1>
        <p className="mt-2 text-muted-foreground">
          We couldn&apos;t find this order. It may have expired or the link is invalid.
        </p>
        <Link
          href="/products"
          className="mt-6 inline-flex h-10 items-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          Continue Shopping
        </Link>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-lg px-4 py-16 text-center sm:px-6">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
        <CheckCircle className="h-8 w-8 text-green-600 dark:text-green-400" />
      </div>

      <h1 className="mt-6 text-3xl font-bold tracking-tight text-foreground">
        Order Confirmed!
      </h1>
      <p className="mt-2 text-muted-foreground">
        Thank you for your purchase. Your order number is{" "}
        <span className="font-semibold text-foreground">{order.orderNumber || orderId}</span>.
      </p>

      <div className="mt-8 rounded-lg border border-border bg-card p-6 text-left">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          Order Summary
        </h2>
        <ul className="mt-4 divide-y divide-border">
          {order.items?.map((item: any, idx: number) => (
            <li key={idx} className="flex items-center justify-between py-3">
              <div>
                <p className="text-sm font-medium text-foreground">{item.name}</p>
                <p className="text-xs text-muted-foreground">Qty: {item.quantity}</p>
              </div>
              <p className="text-sm font-medium text-foreground">
                {formatPrice(item.price * item.quantity)}
              </p>
            </li>
          ))}
        </ul>
        <div className="mt-4 flex items-center justify-between border-t border-border pt-4">
          <span className="text-sm font-semibold text-foreground">Total</span>
          <span className="text-sm font-semibold text-foreground">
            {formatPrice(order.total)}
          </span>
        </div>
      </div>

      <Link
        href="/products"
        className="mt-8 inline-flex h-10 items-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
      >
        Continue Shopping
      </Link>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CHECKOUT_CANCEL_PAGE = `"use client";

import Link from "next/link";
import { XCircle } from "lucide-react";

export default function CheckoutCancelPage() {
  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center px-4 text-center">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
        <XCircle className="h-8 w-8 text-red-600 dark:text-red-400" />
      </div>

      <h1 className="mt-6 text-2xl font-bold tracking-tight text-foreground">
        Payment Cancelled
      </h1>
      <p className="mx-auto mt-2 max-w-md text-muted-foreground">
        Your payment was not completed. You can try again or continue shopping.
      </p>

      <div className="mt-8 flex flex-col gap-3 sm:flex-row">
        <Link
          href="/checkout"
          className="inline-flex h-10 items-center justify-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          Try Again
        </Link>
        <Link
          href="/products"
          className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-background px-5 text-sm font-medium text-foreground transition-colors hover:bg-accent"
        >
          Continue Shopping
        </Link>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCTS_PAGE = `"use client";

import React, { useState, useMemo } from "react";
import { Search, SlidersHorizontal, ChevronDown } from "lucide-react";
import { useProducts, useCategories } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/ecommerce/empty-state";

type SortOption = "newest" | "price-asc" | "price-desc" | "name";

export default function ProductsPage() {
  const { products, isLoading } = useProducts();
  const { categories } = useCategories();
  const [search, setSearch] = useState("");
  const [selectedCategory, setSelectedCategory] = useState<string | null>(null);
  const [sort, setSort] = useState<SortOption>("newest");
  const [showSort, setShowSort] = useState(false);

  const sortLabels: Record<SortOption, string> = {
    newest: "Newest",
    "price-asc": "Price: Low to High",
    "price-desc": "Price: High to Low",
    name: "Name A–Z",
  };

  const filtered = useMemo(() => {
    let result = products ?? [];

    if (search.trim()) {
      const q = search.toLowerCase();
      result = result.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          (p.description && p.description.toLowerCase().includes(q))
      );
    }

    if (selectedCategory) {
      result = result.filter(
        (p) => p.category_slug === selectedCategory || p.category === selectedCategory
      );
    }

    switch (sort) {
      case "price-asc":
        result = [...result].sort((a, b) => a.price_in_cents - b.price_in_cents);
        break;
      case "price-desc":
        result = [...result].sort((a, b) => b.price_in_cents - a.price_in_cents);
        break;
      case "name":
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      default:
        break;
    }

    return result;
  }, [products, search, selectedCategory, sort]);

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
          All Products
        </h1>
        <p className="mt-2 text-muted-foreground">
          Browse our complete collection
        </p>
      </div>

      {/* Search & Sort Bar */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative flex-1 sm:max-w-sm">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search products..."
            className="h-10 w-full rounded-lg border border-border bg-background pl-10 pr-4 text-sm text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
          />
        </div>

        <div className="relative">
          <button
            onClick={() => setShowSort(!showSort)}
            className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-accent"
          >
            <SlidersHorizontal className="h-4 w-4" />
            {sortLabels[sort]}
            <ChevronDown className="h-4 w-4" />
          </button>
          {showSort && (
            <div className="absolute right-0 top-full z-20 mt-1 w-48 rounded-lg border border-border bg-background py-1 shadow-lg">
              {(Object.entries(sortLabels) as [SortOption, string][]).map(([key, label]) => (
                <button
                  key={key}
                  onClick={() => {
                    setSort(key);
                    setShowSort(false);
                  }}
                  className={"w-full px-4 py-2 text-left text-sm transition-colors hover:bg-accent " + (sort === key ? "font-medium text-primary" : "text-foreground")}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Category Filter Pills */}
      {categories && categories.length > 0 && (
        <div className="mb-8 flex flex-wrap gap-2">
          <button
            onClick={() => setSelectedCategory(null)}
            className={"rounded-full px-4 py-1.5 text-sm font-medium transition-colors " + (selectedCategory === null ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-accent hover:text-foreground")}
          >
            All
          </button>
          {categories.map((cat) => (
            <button
              key={cat.id}
              onClick={() => setSelectedCategory(selectedCategory === cat.slug ? null : cat.slug)}
              className={"rounded-full px-4 py-1.5 text-sm font-medium transition-colors " + (selectedCategory === cat.slug ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-accent hover:text-foreground")}
            >
              {cat.name}
            </button>
          ))}
        </div>
      )}

      {/* Product Grid */}
      {isLoading ? (
        <div className="grid grid-cols-2 gap-4 sm:gap-6 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-square w-full rounded-xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Search}
          title="No products found"
          description={search ? "Try adjusting your search or filters." : "Check back soon for new arrivals."}
          action={search || selectedCategory ? { label: "Clear Filters", href: "/products" } : undefined}
        />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:gap-6 lg:grid-cols-3 xl:grid-cols-4">
          {filtered.map((product) => (
            <ProductCard key={product.id} product={product} />
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRODUCT_DETAIL_PAGE = `"use client";

import React, { useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { ChevronRight, Minus, Plus, ShoppingCart, Truck, RotateCcw, Shield } from "lucide-react";
import { useProduct, useCart, formatPrice } from "@/lib/wemob-store";
import { ProductGallery } from "@/components/ecommerce/product-gallery";
import { PriceDisplay } from "@/components/ecommerce/price-display";
import { ReviewList } from "@/components/ecommerce/review-list";
import { WishlistButton } from "@/components/ecommerce/wishlist-button";
import { Skeleton } from "@/components/ui/skeleton";

export default function ProductDetailPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { product, isLoading } = useProduct(slug);
  const { addItem } = useCart();
  const [quantity, setQuantity] = useState(1);
  const [isAdding, setIsAdding] = useState(false);
  const [addedToCart, setAddedToCart] = useState(false);

  const handleAddToCart = async () => {
    if (!product || product.in_stock === false) return;
    setIsAdding(true);
    try {
      await addItem(product.id, quantity);
      setAddedToCart(true);
      setTimeout(() => setAddedToCart(false), 2000);
    } catch {
      // handled by cart provider
    } finally {
      setIsAdding(false);
    }
  };

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <Skeleton className="mb-6 h-5 w-64" />
        <div className="grid gap-8 lg:grid-cols-2">
          <Skeleton className="aspect-square w-full rounded-xl" />
          <div className="space-y-4">
            <Skeleton className="h-8 w-3/4" />
            <Skeleton className="h-6 w-1/3" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        </div>
      </div>
    );
  }

  if (!product) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center px-4 text-center">
        <h1 className="text-2xl font-bold text-foreground">Product not found</h1>
        <p className="mt-2 text-muted-foreground">The product you are looking for does not exist.</p>
        <Link
          href="/products"
          className="mt-6 inline-flex h-10 items-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          Browse Products
        </Link>
      </div>
    );
  }

  const outOfStock = product.in_stock === false;

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-1.5 text-sm text-muted-foreground">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        {product.category && (
          <>
            <ChevronRight className="h-3.5 w-3.5" />
            <Link href={"/categories/" + (product.category_slug || product.category)} className="transition-colors hover:text-foreground">
              {product.category}
            </Link>
          </>
        )}
        <ChevronRight className="h-3.5 w-3.5" />
        <span className="truncate font-medium text-foreground">{product.name}</span>
      </nav>

      <div className="grid gap-8 lg:grid-cols-2 lg:gap-12">
        {/* Image Gallery */}
        <ProductGallery images={product.images || []} name={product.name} />

        {/* Product Info */}
        <div>
          {product.category && (
            <p className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
              {product.category}
            </p>
          )}
          <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
            {product.name}
          </h1>

          <div className="mt-4 flex items-center gap-3">
            <PriceDisplay product={product} size="lg" />
            <WishlistButton productId={product.id} />
          </div>

          {product.description && (
            <div className="mt-6 text-sm leading-relaxed text-muted-foreground">
              <p>{product.description}</p>
            </div>
          )}

          {/* Quantity & Add to Cart */}
          <div className="mt-8 space-y-4">
            {!outOfStock && (
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-foreground">Quantity</span>
                <div className="flex items-center rounded-lg border border-border">
                  <button
                    onClick={() => setQuantity(Math.max(1, quantity - 1))}
                    className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
                    aria-label="Decrease quantity"
                  >
                    <Minus className="h-4 w-4" />
                  </button>
                  <span className="flex h-10 w-12 items-center justify-center text-sm font-medium text-foreground">
                    {quantity}
                  </span>
                  <button
                    onClick={() => setQuantity(quantity + 1)}
                    className="flex h-10 w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
                    aria-label="Increase quantity"
                  >
                    <Plus className="h-4 w-4" />
                  </button>
                </div>
              </div>
            )}

            <button
              onClick={handleAddToCart}
              disabled={outOfStock || isAdding}
              className="flex h-12 w-full items-center justify-center gap-2 rounded-lg bg-primary text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {outOfStock ? (
                "Out of Stock"
              ) : addedToCart ? (
                "Added to Cart!"
              ) : (
                <>
                  <ShoppingCart className="h-4 w-4" />
                  {isAdding ? "Adding..." : "Add to Cart"}
                </>
              )}
            </button>
          </div>

          {/* Trust Badges */}
          <div className="mt-8 grid grid-cols-3 gap-4 border-t border-border pt-6">
            <div className="flex flex-col items-center gap-1.5 text-center">
              <Truck className="h-5 w-5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Free Shipping</span>
            </div>
            <div className="flex flex-col items-center gap-1.5 text-center">
              <RotateCcw className="h-5 w-5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Easy Returns</span>
            </div>
            <div className="flex flex-col items-center gap-1.5 text-center">
              <Shield className="h-5 w-5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Secure Payment</span>
            </div>
          </div>
        </div>
      </div>

      {/* Reviews Section */}
      <div className="mt-16">
        <ReviewList productId={product.id} />
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_CATEGORY_PAGE = `"use client";

import React from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { ChevronRight } from "lucide-react";
import { useProducts, useCategories } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";
import { Skeleton } from "@/components/ui/skeleton";

export default function CategoryPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { products, isLoading: productsLoading } = useProducts();
  const { categories, isLoading: categoriesLoading } = useCategories();

  const isLoading = productsLoading || categoriesLoading;
  const category = categories?.find((c) => c.slug === slug);
  const filtered = products?.filter(
    (p) => p.category_slug === slug || p.category === category?.name
  ) ?? [];

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <Skeleton className="mb-2 h-5 w-48" />
        <Skeleton className="mb-8 h-9 w-64" />
        <div className="grid grid-cols-2 gap-4 sm:gap-6 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-square w-full rounded-xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const categoryName = category?.name ?? slug.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-1.5 text-sm text-muted-foreground">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <span className="font-medium text-foreground">{categoryName}</span>
      </nav>

      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
          {categoryName}
        </h1>
        {category?.description && (
          <p className="mt-2 text-muted-foreground">{category.description}</p>
        )}
        <p className="mt-1 text-sm text-muted-foreground">
          {filtered.length} {filtered.length === 1 ? "product" : "products"}
        </p>
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          icon={ChevronRight}
          title="No products in this category"
          description="Check back soon for new arrivals in this category."
          action={{ label: "Browse All Products", href: "/products" }}
        />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:gap-6 lg:grid-cols-3 xl:grid-cols-4">
          {filtered.map((product) => (
            <ProductCard key={product.id} product={product} />
          ))}
        </div>
      )}
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_SHIPPING_POLICY = `"use client";

import { useStoreConfig } from "@/lib/wemob-store";

export default function ShippingPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 lg:px-8">
      <h1 className="text-3xl font-bold tracking-tight text-foreground">Shipping Policy</h1>
      <p className="mt-2 text-sm text-muted-foreground">Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}</p>

      <div className="mt-8 space-y-8 text-sm leading-relaxed text-muted-foreground">
        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Processing Time</h2>
          <p>All orders placed with {storeName} are processed within 1–3 business days. Orders placed on weekends or holidays will be processed on the next business day. You will receive a confirmation email with tracking information once your order has shipped.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Shipping Rates & Delivery Times</h2>
          <div className="overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wide text-foreground">Method</th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wide text-foreground">Estimated Delivery</th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wide text-foreground">Cost</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                <tr>
                  <td className="px-4 py-3">Standard Shipping</td>
                  <td className="px-4 py-3">5–7 business days</td>
                  <td className="px-4 py-3">Free on orders over $50</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">Express Shipping</td>
                  <td className="px-4 py-3">2–3 business days</td>
                  <td className="px-4 py-3">$9.99</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">Overnight Shipping</td>
                  <td className="px-4 py-3">1 business day</td>
                  <td className="px-4 py-3">$19.99</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">International Shipping</h2>
          <p>We currently ship to select international destinations. International shipping rates and delivery times vary by location and will be calculated at checkout. Please note that international orders may be subject to customs duties and taxes, which are the responsibility of the recipient.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Order Tracking</h2>
          <p>Once your order ships, you will receive an email with a tracking number. You can use this number to track your package on the carrier's website. If you have any questions about your shipment, please don't hesitate to contact our support team.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Shipping Restrictions</h2>
          <p>Some items may have shipping restrictions due to size, weight, or destination regulations. If an item cannot be shipped to your address, you will be notified during checkout. We are unable to ship to P.O. boxes for express and overnight orders.</p>
        </section>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_RETURNS_POLICY = `"use client";

import { useStoreConfig } from "@/lib/wemob-store";

export default function ReturnsPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 lg:px-8">
      <h1 className="text-3xl font-bold tracking-tight text-foreground">Return & Exchange Policy</h1>
      <p className="mt-2 text-sm text-muted-foreground">Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}</p>

      <div className="mt-8 space-y-8 text-sm leading-relaxed text-muted-foreground">
        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Our Return Promise</h2>
          <p>At {storeName}, we want you to be completely satisfied with your purchase. If for any reason you are not happy with your order, we offer a hassle-free return and exchange policy within 30 days of delivery.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Eligibility</h2>
          <ul className="ml-4 list-disc space-y-1.5">
            <li>Items must be returned within 30 days of delivery.</li>
            <li>Products must be in their original condition, unworn, unwashed, and with all tags attached.</li>
            <li>Items must be in their original packaging when possible.</li>
            <li>Sale items and gift cards are final sale and cannot be returned.</li>
            <li>Personalized or custom-made items are non-returnable.</li>
          </ul>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">How to Return</h2>
          <ol className="ml-4 list-decimal space-y-1.5">
            <li>Contact our support team to initiate a return request.</li>
            <li>You will receive a return authorization and prepaid shipping label via email.</li>
            <li>Pack the item securely in its original packaging.</li>
            <li>Attach the shipping label and drop off the package at the designated carrier.</li>
            <li>Once we receive and inspect the item, your refund will be processed within 5–7 business days.</li>
          </ol>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Exchanges</h2>
          <p>If you would like to exchange an item for a different size or color, please follow the return process above and place a new order for the desired item. This ensures the fastest processing time.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Refunds</h2>
          <p>Refunds will be issued to the original payment method. Please allow 5–7 business days for the refund to appear on your statement after we have received and processed your return. Shipping costs are non-refundable unless the return is due to a defective or incorrect item.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Damaged or Defective Items</h2>
          <p>If you received a damaged or defective item, please contact us within 48 hours of delivery with photos of the issue. We will arrange a replacement or full refund at no additional cost to you.</p>
        </section>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_PRIVACY_POLICY = `"use client";

import { useStoreConfig } from "@/lib/wemob-store";

export default function PrivacyPolicyPage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 lg:px-8">
      <h1 className="text-3xl font-bold tracking-tight text-foreground">Privacy Policy</h1>
      <p className="mt-2 text-sm text-muted-foreground">Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}</p>

      <div className="mt-8 space-y-8 text-sm leading-relaxed text-muted-foreground">
        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Introduction</h2>
          <p>{storeName} ("we", "our", or "us") is committed to protecting your privacy. This Privacy Policy explains how we collect, use, disclose, and safeguard your information when you visit our website and make purchases from our store.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Information We Collect</h2>
          <p className="mb-2">We may collect information about you in a variety of ways, including:</p>
          <ul className="ml-4 list-disc space-y-1.5">
            <li><strong className="text-foreground">Personal Data:</strong> Name, email address, shipping address, phone number, and payment information provided when you place an order.</li>
            <li><strong className="text-foreground">Order Data:</strong> Products purchased, order history, and transaction details.</li>
            <li><strong className="text-foreground">Usage Data:</strong> Browser type, IP address, pages visited, and time spent on our website, collected automatically through cookies and similar technologies.</li>
            <li><strong className="text-foreground">Communication Data:</strong> Information you provide when contacting our support team or subscribing to our newsletter.</li>
          </ul>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">How We Use Your Information</h2>
          <ul className="ml-4 list-disc space-y-1.5">
            <li>Process and fulfill your orders, including shipping and payment processing.</li>
            <li>Send order confirmations, shipping updates, and receipts.</li>
            <li>Respond to your inquiries and provide customer support.</li>
            <li>Send promotional emails and newsletters (with your consent).</li>
            <li>Improve our website, products, and services.</li>
            <li>Prevent fraudulent transactions and protect against unauthorized activity.</li>
          </ul>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Cookies</h2>
          <p>We use cookies and similar tracking technologies to enhance your browsing experience, analyze site traffic, and personalize content. You can control cookie preferences through your browser settings. Disabling cookies may affect some website functionality.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Data Sharing</h2>
          <p>We do not sell your personal information. We may share your data with trusted third-party service providers who assist us in operating our website, processing payments, and delivering orders. These providers are obligated to protect your information and use it only for the purposes we specify.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Data Security</h2>
          <p>We implement industry-standard security measures to protect your personal information, including SSL encryption for all data transmission. However, no method of internet transmission is 100% secure, and we cannot guarantee absolute security.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Your Rights</h2>
          <p>Depending on your location, you may have the right to access, correct, delete, or port your personal data. You may also opt out of marketing communications at any time by clicking the unsubscribe link in our emails or contacting us directly.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Contact Us</h2>
          <p>If you have questions about this Privacy Policy or our data practices, please contact us through our contact page.</p>
        </section>
      </div>
    </div>
  );
}
`

const ECOMMERCE_TEMPLATE_TERMS_POLICY = `"use client";

import { useStoreConfig } from "@/lib/wemob-store";

export default function TermsOfServicePage() {
  const { config } = useStoreConfig();
  const storeName = config?.name || "Our Store";

  return (
    <div className="mx-auto max-w-3xl px-4 py-12 sm:px-6 lg:px-8">
      <h1 className="text-3xl font-bold tracking-tight text-foreground">Terms of Service</h1>
      <p className="mt-2 text-sm text-muted-foreground">Last updated: {new Date().toLocaleDateString("en-US", { month: "long", year: "numeric" })}</p>

      <div className="mt-8 space-y-8 text-sm leading-relaxed text-muted-foreground">
        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Acceptance of Terms</h2>
          <p>By accessing and using the {storeName} website, you agree to be bound by these Terms of Service and all applicable laws and regulations. If you do not agree with any of these terms, you are prohibited from using this site.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Products & Pricing</h2>
          <p>All product descriptions, images, and prices are subject to change without notice. We reserve the right to modify or discontinue any product at any time. Prices are displayed in the store's configured currency and include applicable taxes unless otherwise stated. We make every effort to ensure pricing accuracy, but errors may occur. In the event of a pricing error, we reserve the right to cancel or refuse orders placed at the incorrect price.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Orders & Payment</h2>
          <p>By placing an order, you represent that the information provided is accurate and that you are authorized to use the payment method. We reserve the right to refuse or cancel any order for reasons including product availability, errors in pricing or product information, or suspected fraudulent activity.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Intellectual Property</h2>
          <p>All content on this website, including text, graphics, logos, images, and software, is the property of {storeName} or its content suppliers and is protected by intellectual property laws. You may not reproduce, distribute, modify, or create derivative works from any content without our express written permission.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">User Accounts</h2>
          <p>If you create an account, you are responsible for maintaining the confidentiality of your account credentials and for all activities under your account. You agree to notify us immediately of any unauthorized use of your account. We reserve the right to terminate accounts that violate these terms.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Limitation of Liability</h2>
          <p>{storeName} shall not be liable for any indirect, incidental, special, consequential, or punitive damages resulting from your use of or inability to use the website or products. Our total liability for any claim arising from these terms shall not exceed the amount you paid for the product giving rise to the claim.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Governing Law</h2>
          <p>These Terms of Service shall be governed by and construed in accordance with applicable local laws, without regard to conflict of law principles.</p>
        </section>

        <section>
          <h2 className="mb-3 text-lg font-semibold text-foreground">Changes to Terms</h2>
          <p>We reserve the right to update these Terms of Service at any time. Changes will be effective immediately upon posting. Your continued use of the website after changes constitutes acceptance of the updated terms.</p>
        </section>
      </div>
    </div>
  );
}
`
